package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"oxygen/worker/internal/db"
	"oxygen/worker/internal/transcode"

	"github.com/redis/go-redis/v9"
)

type Job struct {
	ID             string  `json:"id"`
	OrganizationID string  `json:"organization_id"`
	FolderID       *string `json:"folder_id"`
	Title          string  `json:"title"`
	FileName       *string `json:"file_name"`
	FilePath       *string `json:"file_path"`
	SourceURL      *string `json:"source_url"`
	StreamingURL   *string `json:"streaming_url"`
	Size           int64   `json:"size"`
	Status         string  `json:"status"`
	Progress       int     `json:"progress"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type WebhookEvent struct {
	OrganizationID string   `json:"organization_id"`
	Event          string   `json:"event"`
	Title          string   `json:"title"`
	FileName       string   `json:"file_name"`
	Status         string   `json:"status"`
	Tags           []string `json:"tags"`
}

type S3Client interface {
	DownloadSource(ctx context.Context, key, destPath string) error
	UploadHLS(ctx context.Context, localDir, orgID, mediaFileID string) error
	StreamingURL(orgID, mediaFileID string) string
}

type Consumer struct {
	rdb             *redis.Client
	queueKey        string
	webhookQueueKey string
	workerID        int
	log             *slog.Logger
	store           *db.Store
	s3              S3Client
	transcoder      *transcode.Transcoder
	workDir         string
}

func NewConsumer(rdb *redis.Client, queueKey string, workerID int, store *db.Store, s3 S3Client, tx *transcode.Transcoder, workDir string) *Consumer {
	return &Consumer{
		rdb:             rdb,
		queueKey:        queueKey,
		webhookQueueKey: queueKey + ":webhooks",
		workerID:        workerID,
		log:             slog.With("worker_id", workerID, "queue_key", queueKey),
		store:           store,
		s3:              s3,
		transcoder:      tx,
		workDir:         workDir,
	}
}

func (c *Consumer) Run(ctx context.Context) {
	const brpopTimeout = 30 * time.Second

	c.log.Info("consumer started")
	for {
		if ctx.Err() != nil {
			c.log.Info("consumer stopping")
			return
		}

		res, err := c.rdb.BRPop(ctx, brpopTimeout, c.queueKey).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			c.log.Error("brpop failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		if len(res) < 2 {
			c.log.Error("brpop returned unexpected payload", "res", res)
			continue
		}

		raw := res[1]
		c.handle(ctx, raw)
	}
}

func (c *Consumer) handle(ctx context.Context, raw string) {
	var job Job
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		c.log.Error("decode job failed", "err", err, "raw", raw)
		return
	}

	if job.ID == "" || job.OrganizationID == "" {
		c.log.Error("job missing required fields", "job", job)
		return
	}

	c.log.Info("job received",
		"media_file_id", job.ID,
		"organization_id", job.OrganizationID,
		"title", job.Title,
		"file_path", strOrEmpty(job.FilePath),
		"size", job.Size,
		"status", job.Status,
	)

	mediaFile, err := c.store.LoadMediaFile(ctx, job.ID, job.OrganizationID)
	if err != nil {
		c.log.Error("load media_file failed", "err", err, "media_file_id", job.ID)
		return
	}
	if mediaFile == nil {
		c.log.Error("media_file not found or org mismatch", "media_file_id", job.ID, "organization_id", job.OrganizationID)
		return
	}

	profile, err := c.store.LoadMediaFileProfile(ctx, job.ID)
	if err != nil {
		c.log.Error("load profile failed", "err", err, "media_file_id", job.ID)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}
	if profile == nil {
		c.log.Error("no profile found for media_file", "media_file_id", job.ID)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}

	c.log.Info("profile loaded",
		"media_file_id", job.ID,
		"profile_name", profile.Name,
		"qualities", profile.Qualities,
	)

	if err := c.store.UpdateProgress(ctx, job.ID, "progress", 0); err != nil {
		c.log.Error("set progress=0 failed", "err", err)
	} else {
		c.pushWebhookEvent(ctx, job, mediaFile, "progress")
	}

	jobDir := filepath.Join(c.workDir, job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		c.log.Error("create work dir failed", "err", err, "path", jobDir)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}
	defer os.RemoveAll(jobDir)

	outputDir := filepath.Join(jobDir, "hls")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		c.log.Error("create output dir failed", "err", err, "path", outputDir)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}

	var sourcePath string
	if mediaFile.FilePath != nil && *mediaFile.FilePath != "" {
		sourcePath = filepath.Join(jobDir, "source"+filepath.Ext(*mediaFile.FilePath))
		if err := c.s3.DownloadSource(ctx, *mediaFile.FilePath, sourcePath); err != nil {
			c.log.Error("download source failed", "err", err)
			c.markFailedWithWebhook(ctx, job, mediaFile, 0)
			return
		}
	} else if mediaFile.SourceURL != nil && *mediaFile.SourceURL != "" {
		sourcePath = *mediaFile.SourceURL
	} else {
		c.log.Error("no source file_path or source_url", "media_file_id", job.ID)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}

	err = c.transcoder.Run(ctx, sourcePath, profile.Qualities, outputDir, func(pct int) {
		if dbErr := c.store.UpdateProgress(ctx, job.ID, "progress", pct); dbErr != nil {
			c.log.Error("update progress failed", "err", dbErr, "pct", pct)
		}
	})

	if err != nil {
		c.log.Error("transcode failed", "err", err, "media_file_id", job.ID)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}

	if err := c.s3.UploadHLS(ctx, outputDir, job.OrganizationID, job.ID); err != nil {
		c.log.Error("s3 upload failed", "err", err, "media_file_id", job.ID)
		c.markFailedWithWebhook(ctx, job, mediaFile, 0)
		return
	}

	streamingURL := c.s3.StreamingURL(job.OrganizationID, job.ID)
	if err := c.store.UpdateSuccess(ctx, job.ID, streamingURL); err != nil {
		c.log.Error("update success failed", "err", err, "media_file_id", job.ID)
		return
	}

	c.log.Info("job completed",
		"media_file_id", job.ID,
		"streaming_url", streamingURL,
	)

	c.pushWebhookEvent(ctx, job, mediaFile, "success")
}

func (c *Consumer) pushWebhookEvent(ctx context.Context, job Job, mediaFile *db.MediaFileRow, status string) {
	evt := WebhookEvent{
		OrganizationID: job.OrganizationID,
		Event:          "file_status_changed",
		Title:          mediaFile.Title,
		FileName:       strOrEmpty(mediaFile.FileName),
		Status:         status,
		Tags:           mediaFile.Tags,
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		c.log.Error("marshal webhook event failed", "err", err)
		return
	}

	if err := c.rdb.LPush(ctx, c.webhookQueueKey, payload).Err(); err != nil {
		c.log.Error("lpush webhook event failed", "err", err)
	}
}

func (c *Consumer) markFailed(ctx context.Context, mediaFileID string, progress int) error {
	if err := c.store.UpdateFailed(ctx, mediaFileID, progress); err != nil {
		c.log.Error("mark failed error", "err", err, "media_file_id", mediaFileID)
		return err
	}
	return nil
}

func (c *Consumer) markFailedWithWebhook(ctx context.Context, job Job, mediaFile *db.MediaFileRow, progress int) {
	if err := c.markFailed(ctx, job.ID, progress); err == nil {
		c.pushWebhookEvent(ctx, job, mediaFile, "failed")
	}
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
