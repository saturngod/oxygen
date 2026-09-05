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
	"oxygen/worker/internal/thumbnail"
	"oxygen/worker/internal/transcode"

	"github.com/redis/go-redis/v9"
)

type Job struct {
	ID                string  `json:"id"`
	OrganizationID    string  `json:"organization_id"`
	FolderID          *string `json:"folder_id"`
	Title             string  `json:"title"`
	FileName          *string `json:"file_name"`
	FilePath          *string `json:"file_path"`
	SourceURL         *string `json:"source_url"`
	StreamingURL      *string `json:"streaming_url"`
	Size              int64   `json:"size"`
	Status            string  `json:"status"`
	Progress          int     `json:"progress"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	GenerateThumbnail bool    `json:"generate_thumbnail"`
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
	UploadThumbnails(ctx context.Context, localDir, orgID, mediaFileID string) error
	UploadPoster(ctx context.Context, localPath, orgID, mediaFileID string) error
	StreamingURL(orgID, mediaFileID string) string
}

type Consumer struct {
	rdb                  *redis.Client
	queueKey             string
	webhookQueueKey      string
	workerID             int
	log                  *slog.Logger
	store                *db.Store
	s3                   S3Client
	transcoder           *transcode.Transcoder
	workDir              string
	thumbnailConfig      thumbnail.Config
	thumbnailJPEGQuality int
	thumbnailPosterWidth int
}

func NewConsumer(rdb *redis.Client, queueKey string, workerID int, store *db.Store, s3 S3Client, tx *transcode.Transcoder, workDir string, thumbnailConfig thumbnail.Config, thumbnailJPEGQuality, thumbnailPosterWidth int) *Consumer {
	return &Consumer{
		rdb:                  rdb,
		queueKey:             queueKey,
		webhookQueueKey:      queueKey + ":webhooks",
		workerID:             workerID,
		log:                  slog.With("worker_id", workerID, "queue_key", queueKey),
		store:                store,
		s3:                   s3,
		transcoder:           tx,
		workDir:              workDir,
		thumbnailConfig:      thumbnailConfig,
		thumbnailJPEGQuality: thumbnailJPEGQuality,
		thumbnailPosterWidth: thumbnailPosterWidth,
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
		c.handle(claimedJobContext(ctx), raw)
	}
}

// claimedJobContext lets shutdown cancel the blocking Redis poll without
// canceling a job that has already been removed from the queue. The process
// manager gives claimed jobs time to finish before it force-kills the worker.
func claimedJobContext(pollContext context.Context) context.Context {
	return context.WithoutCancel(pollContext)
}

func (c *Consumer) handle(ctx context.Context, raw string) {
	var job Job
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		c.log.Error("decode job failed", "err", err)
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
		"generate_thumbnail", job.GenerateThumbnail,
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
		"video_segment_duration_seconds", profile.VideoSegmentDurationSeconds,
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

	mediaInfo, mediaProbeErr := c.transcoder.ProbeMedia(ctx, sourcePath)
	mediaInfoValid := mediaProbeErr == nil && mediaInfo.Duration > 0
	duration := mediaInfo.Duration
	if !mediaInfoValid {
		c.log.Warn("media probe failed, using progress fallback", "err", mediaProbeErr, "media_file_id", job.ID)
		if job.GenerateThumbnail {
			thumbnailErr := mediaProbeErr
			if thumbnailErr == nil {
				thumbnailErr = errors.New("probed media information is invalid")
			}
			c.log.Error("thumbnail_generation_failed", "error", thumbnailErr, "media_file_id", job.ID, "organization_id", job.OrganizationID)
		}
		duration = 1
	}

	var thumbnailOptions *transcode.ThumbnailOptions
	thumbnailDir := filepath.Join(jobDir, "thumbnails")
	thumbnailStartedAt := time.Now()
	if job.GenerateThumbnail && mediaInfoValid {
		jobThumbnailConfig := c.thumbnailConfig
		derivedHeight, heightErr := thumbnail.HeightForAspectRatio(jobThumbnailConfig.Width, mediaInfo.DisplayAspectRatio)
		posterHeight, posterHeightErr := thumbnail.HeightForAspectRatio(c.thumbnailPosterWidth, mediaInfo.DisplayAspectRatio)
		jobThumbnailConfig.Height = derivedHeight
		plan, planErr := thumbnail.BuildPlan(duration, jobThumbnailConfig)
		if heightErr == nil && derivedHeight*jobThumbnailConfig.Rows > 8192 {
			heightErr = errors.New("derived thumbnail storyboard height exceeds 8192 pixels")
		}
		if posterHeightErr == nil && posterHeight > 8192 {
			posterHeightErr = errors.New("derived poster height exceeds 8192 pixels")
		}
		if heightErr != nil {
			c.log.Error("thumbnail_generation_failed", "error", heightErr, "media_file_id", job.ID, "organization_id", job.OrganizationID)
		} else if posterHeightErr != nil {
			c.log.Error("thumbnail_generation_failed", "error", posterHeightErr, "media_file_id", job.ID, "organization_id", job.OrganizationID)
		} else if planErr != nil {
			c.log.Error("thumbnail_generation_failed", "error", planErr, "media_file_id", job.ID, "organization_id", job.OrganizationID)
		} else if mkdirErr := os.MkdirAll(thumbnailDir, 0o755); mkdirErr != nil {
			c.log.Error("thumbnail_generation_failed", "error", mkdirErr, "media_file_id", job.ID, "organization_id", job.OrganizationID)
		} else {
			thumbnailOptions = &transcode.ThumbnailOptions{
				StoryboardOutputPath: filepath.Join(thumbnailDir, thumbnail.StoryboardFilename),
				PosterOutputPath:     filepath.Join(jobDir, thumbnail.PosterFilename),
				PosterWidth:          c.thumbnailPosterWidth,
				PosterHeight:         posterHeight,
				Config:               jobThumbnailConfig,
				Plan:                 plan,
				JPEGQuality:          c.thumbnailJPEGQuality,
			}
			c.log.Info("thumbnail_generation_enabled",
				"media_file_id", job.ID,
				"organization_id", job.OrganizationID,
				"duration_seconds", duration,
				"interval_seconds", jobThumbnailConfig.IntervalSeconds,
				"effective_interval_seconds", plan.EffectiveInterval,
				"storyboard_cell_count", plan.StoryboardCellCount,
				"storyboard_width", plan.Columns*jobThumbnailConfig.Width,
				"storyboard_height", plan.Rows*jobThumbnailConfig.Height,
				"poster_width", c.thumbnailPosterWidth,
				"poster_height", posterHeight,
			)
		}
	}

	err = c.transcoder.Run(ctx, sourcePath, profile.Qualities, outputDir, duration, profile.VideoSegmentDurationSeconds, thumbnailOptions, func(pct int) {
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

	if thumbnailOptions != nil {
		storyboardBytes, thumbnailErr := thumbnail.ValidateAndWriteVTT(thumbnailDir, duration, thumbnailOptions.Config, thumbnailOptions.Plan)
		if thumbnailErr != nil {
			c.log.Error("thumbnail_generation_failed",
				"error", thumbnailErr,
				"media_file_id", job.ID,
				"organization_id", job.OrganizationID,
			)
		} else {
			c.log.Info("thumbnail_generation_completed",
				"media_file_id", job.ID,
				"organization_id", job.OrganizationID,
				"output_bytes", storyboardBytes,
				"elapsed_ms", time.Since(thumbnailStartedAt).Milliseconds(),
			)
			if uploadErr := c.s3.UploadThumbnails(ctx, thumbnailDir, job.OrganizationID, job.ID); uploadErr != nil {
				c.log.Error("thumbnail_upload_failed",
					"error", uploadErr,
					"media_file_id", job.ID,
					"organization_id", job.OrganizationID,
				)
			} else {
				c.log.Info("thumbnail_upload_completed",
					"media_file_id", job.ID,
					"organization_id", job.OrganizationID,
				)
			}
		}

		posterBytes, posterErr := thumbnail.ValidatePoster(thumbnailOptions.PosterOutputPath)
		if posterErr != nil {
			c.log.Error("poster_generation_failed",
				"error", posterErr,
				"media_file_id", job.ID,
				"organization_id", job.OrganizationID,
			)
		} else if uploadErr := c.s3.UploadPoster(ctx, thumbnailOptions.PosterOutputPath, job.OrganizationID, job.ID); uploadErr != nil {
			c.log.Error("poster_upload_failed",
				"error", uploadErr,
				"media_file_id", job.ID,
				"organization_id", job.OrganizationID,
			)
		} else {
			c.log.Info("poster_upload_completed",
				"media_file_id", job.ID,
				"organization_id", job.OrganizationID,
				"output_bytes", posterBytes,
			)
		}
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
