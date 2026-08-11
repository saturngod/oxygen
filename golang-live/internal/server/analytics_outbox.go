package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type analyticsOutboxEnvelope struct {
	Batch     AnalyticsEventBatch `json:"batch"`
	CreatedAt time.Time           `json:"created_at"`
}

type AnalyticsOutbox struct {
	root   string
	client *AnalyticsClient
	log    *slog.Logger
	wake   chan struct{}
}

func NewAnalyticsOutbox(root string, client *AnalyticsClient, log *slog.Logger) *AnalyticsOutbox {
	return &AnalyticsOutbox{root: root, client: client, log: log, wake: make(chan struct{}, 1)}
}

func (o *AnalyticsOutbox) Prepare() error {
	if strings.TrimSpace(o.root) == "" {
		return fmt.Errorf("ANALYTICS_OUTBOX_ROOT must not be empty")
	}
	if err := os.MkdirAll(o.root, 0o700); err != nil {
		return fmt.Errorf("create analytics outbox: %w", err)
	}
	return nil
}

func (o *AnalyticsOutbox) Enqueue(batch AnalyticsEventBatch) error {
	if len(batch.Events) == 0 {
		return nil
	}
	if err := o.Prepare(); err != nil {
		return err
	}
	body, err := json.Marshal(analyticsOutboxEnvelope{Batch: batch, CreatedAt: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("marshal analytics outbox entry: %w", err)
	}
	finalPath := filepath.Join(o.root, time.Now().UTC().Format("20060102T150405.000000000")+"-"+randomHex(8)+".json")
	temporaryPath := finalPath + ".tmp"
	if err := writeDurableFile(temporaryPath, body); err != nil {
		return fmt.Errorf("write analytics outbox entry: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("commit analytics outbox entry: %w", err)
	}
	if err := syncDirectory(o.root); err != nil {
		return fmt.Errorf("sync analytics outbox: %w", err)
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}

func (o *AnalyticsOutbox) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		o.flush(ctx)
		select {
		case <-ctx.Done():
			return
		case <-o.wake:
		case <-ticker.C:
		}
	}
}

func (o *AnalyticsOutbox) flush(ctx context.Context) {
	entries, err := os.ReadDir(o.root)
	if err != nil {
		if !os.IsNotExist(err) {
			o.log.Error("analytics outbox read failed", "err", err)
		}
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		entryPath := filepath.Join(o.root, entry.Name())
		body, err := os.ReadFile(entryPath)
		if err != nil {
			o.log.Warn("analytics outbox entry read failed", "err", err, "file", entry.Name())
			continue
		}
		var envelope analyticsOutboxEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			o.log.Error("invalid analytics outbox entry", "err", err, "file", entry.Name())
			continue
		}
		if err := o.client.PostBatch(ctx, envelope.Batch); err != nil {
			o.log.Warn("analytics delivery failed", "err", err, "file", entry.Name())
			return
		}
		if err := os.Remove(entryPath); err != nil {
			o.log.Warn("analytics outbox cleanup failed", "err", err, "file", entry.Name())
			return
		}
		o.log.Info("analytics batch delivered", "file", entry.Name())
	}
}
