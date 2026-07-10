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

type callbackEnvelope struct {
	Path      string          `json:"path"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type CallbackOutbox struct {
	root    string
	laravel *LaravelClient
	log     *slog.Logger
	wake    chan struct{}
}

func NewCallbackOutbox(root string, laravel *LaravelClient, log *slog.Logger) *CallbackOutbox {
	return &CallbackOutbox{
		root:    root,
		laravel: laravel,
		log:     log,
		wake:    make(chan struct{}, 1),
	}
}

func (o *CallbackOutbox) Prepare() error {
	if strings.TrimSpace(o.root) == "" {
		return fmt.Errorf("LIVE_CALLBACK_ROOT must not be empty")
	}

	if err := os.MkdirAll(o.root, 0o700); err != nil {
		return fmt.Errorf("create callback outbox: %w", err)
	}

	return nil
}

func (o *CallbackOutbox) Enqueue(path string, payload any) error {
	if err := o.Prepare(); err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal callback payload: %w", err)
	}

	envelope, err := json.Marshal(callbackEnvelope{
		Path:      path,
		Payload:   body,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("marshal callback envelope: %w", err)
	}

	name := time.Now().UTC().Format("20060102T150405.000000000") + "-" + randomHex(8) + ".json"
	finalPath := filepath.Join(o.root, name)
	temporaryPath := finalPath + ".tmp"

	if err := writeDurableFile(temporaryPath, envelope); err != nil {
		return fmt.Errorf("write callback outbox entry: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("commit callback outbox entry: %w", err)
	}
	if err := syncDirectory(o.root); err != nil {
		return fmt.Errorf("sync callback outbox: %w", err)
	}

	select {
	case o.wake <- struct{}{}:
	default:
	}

	return nil
}

func writeDurableFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()

	return directory.Sync()
}

func (o *CallbackOutbox) Run(ctx context.Context) {
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

func (o *CallbackOutbox) flush(ctx context.Context) {
	entries, err := os.ReadDir(o.root)
	if err != nil {
		if !os.IsNotExist(err) {
			o.log.Error("callback outbox read failed", "err", err)
		}
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

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
			o.log.Warn("callback outbox entry read failed", "err", err, "file", entry.Name())
			continue
		}

		var envelope callbackEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			o.log.Error("invalid callback outbox entry", "err", err, "file", entry.Name())
			continue
		}

		if !json.Valid(envelope.Payload) {
			o.log.Error("invalid callback outbox payload", "file", entry.Name())
			continue
		}

		if err := o.laravel.Post(ctx, envelope.Path, envelope.Payload, nil); err != nil {
			o.log.Warn("callback delivery failed", "err", err, "path", envelope.Path, "file", entry.Name())
			return
		}

		if err := os.Remove(entryPath); err != nil {
			o.log.Warn("delivered callback cleanup failed", "err", err, "file", entry.Name())
			return
		}

		o.log.Info("callback delivered", "path", envelope.Path, "file", entry.Name())
	}
}
