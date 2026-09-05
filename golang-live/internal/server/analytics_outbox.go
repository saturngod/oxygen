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
	"sync"
	"syscall"
	"time"
)

type analyticsOutboxEnvelope struct {
	Batch     AnalyticsEventBatch `json:"batch"`
	CreatedAt time.Time           `json:"created_at"`
}

type AnalyticsOutbox struct {
	enqueueMu sync.Mutex
	root      string
	client    *AnalyticsClient
	log       *slog.Logger
	wake      chan struct{}
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
	o.enqueueMu.Lock()
	defer o.enqueueMu.Unlock()
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
	if err := checkAnalyticsCapacity(o.root, int64(len(body))); err != nil {
		return err
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

// Optional telemetry must leave space for media and mandatory lifecycle events.
// Include dead letters and abandoned temporary files in the storage budget.
func checkAnalyticsCapacity(root string, incoming int64) error {
	const maxBytes = 64 << 20
	const maxFiles = 10000
	const reserveBytes = 256 << 20
	var size int64
	files := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil // A concurrent delivery can remove an entry.
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
			files++
		}
		if size+incoming > maxBytes || files >= maxFiles {
			return fmt.Errorf("analytics backlog limit reached: bytes=%d files=%d", size, files)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return err
	}
	if uint64(stat.Bavail)*uint64(stat.Bsize) < reserveBytes+uint64(incoming) {
		return fmt.Errorf("analytics storage pressure: preserving 256 MiB free space")
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
			if quarantineErr := quarantineOutboxEntry(o.root, entryPath); quarantineErr != nil {
				o.log.Error("invalid analytics outbox entry could not be quarantined", "err", quarantineErr, "file", entry.Name())
				return
			}
			o.log.Error("invalid analytics outbox entry moved to dead letter", "err", err, "file", entry.Name())
			continue
		}
		if err := o.client.PostBatch(ctx, envelope.Batch); err != nil {
			if isPermanentDeliveryError(err) {
				if quarantineErr := quarantineOutboxEntry(o.root, entryPath); quarantineErr != nil {
					o.log.Error("permanent analytics batch could not be quarantined", "err", quarantineErr, "file", entry.Name())
					return
				}
				o.log.Error("permanent analytics batch moved to dead letter", "err", err, "file", entry.Name())
				continue
			}
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
