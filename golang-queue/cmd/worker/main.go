package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"oxygen/worker/internal/config"
	"oxygen/worker/internal/db"
	"oxygen/worker/internal/queue"
	"oxygen/worker/internal/s3"
	"oxygen/worker/internal/transcode"

	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("redis ping failed", "addr", cfg.RedisAddr, "err", err)
		os.Exit(1)
	}

	store, err := db.NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	if cfg.FfmpegVideoCodec == "auto" {
		cfg.FfmpegVideoCodec = transcode.DetectVideoCodec(cfg.FfmpegBin)
	}

	s3Client := s3.NewClient(cfg)

	tx := transcode.NewTranscoder(cfg)

	logger.Info("worker starting",
		"queue_key", cfg.QueueKey,
		"redis_addr", cfg.RedisAddr,
		"database_url_set", cfg.DatabaseURL != "",
		"source_bucket", cfg.SourceBucket,
		"streaming_bucket", cfg.StreamingBucket,
		"concurrency", cfg.Concurrency,
	)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		workerID := i
		go func() {
			defer wg.Done()
			consumer := queue.NewConsumer(rdb, cfg.QueueKey, workerID, store, s3Client, tx, cfg.WorkDir)
			consumer.Run(ctx)
		}()
	}

	<-ctx.Done()
	logger.Info("shutdown signal received, waiting for workers")
	wg.Wait()
	logger.Info("worker stopped cleanly")
}
