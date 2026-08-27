package s3

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"oxygen/worker/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	source    *bucketClient
	streaming *bucketClient
	uploader  objectUploader
	hlsPrefix string
	log       *slog.Logger
}

type objectUploader interface {
	Upload(context.Context, *s3.PutObjectInput, ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}

type bucketClient struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	endpoint  string
	urlPrefix string
	pathStyle bool
}

func newBucketClient(creds aws.CredentialsProvider, region, bucket, endpoint, urlPrefix string, pathStyle bool) *bucketClient {
	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.Credentials = creds
			o.Region = region
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
				o.UsePathStyle = pathStyle
			}
		},
	}

	client := s3.New(s3.Options{}, opts...)
	presigner := s3.NewPresignClient(client)

	return &bucketClient{
		client:    client,
		presigner: presigner,
		bucket:    bucket,
		endpoint:  endpoint,
		urlPrefix: strings.TrimRight(urlPrefix, "/"),
		pathStyle: pathStyle,
	}
}

func NewClient(cfg *config.Config) *Client {
	creds := credentials.NewStaticCredentialsProvider(cfg.AwsAccessKeyID, cfg.AwsSecretAccessKey, "")

	source := newBucketClient(creds, cfg.AwsRegion,
		cfg.SourceBucket, cfg.SourceEndpoint, cfg.SourceURLPrefix, cfg.SourcePathStyle)

	streaming := newBucketClient(creds, cfg.StreamingRegion,
		cfg.StreamingBucket, cfg.StreamingEndpoint, cfg.StreamingURLPrefix, cfg.StreamingPathStyle)
	uploader := manager.NewUploader(streaming.client, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024
	})

	return &Client{
		source:    source,
		streaming: streaming,
		uploader:  uploader,
		hlsPrefix: cfg.HLSPrefix,
		log:       slog.With("component", "s3"),
	}
}

func (c *Client) DownloadSource(ctx context.Context, key, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()

	// Use plain GetObject instead of the concurrent range-request manager.
	// The manager's multipart range downloads + checksum validation are not
	// compatible with MinIO and cause "unexpected EOF" errors.
	resp, err := c.source.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.source.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("download %s: %w", key, err)
	}
	defer resp.Body.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("download %s: %w", key, err)
	}

	c.log.Info("source downloaded", "key", key, "bytes", n, "path", destPath)
	return nil
}

func (c *Client) PresignedSourceURL(ctx context.Context, key string) (string, error) {
	req, err := c.source.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.source.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = 12 * time.Hour
	})
	if err != nil {
		return "", fmt.Errorf("presign get source: %w", err)
	}
	return req.URL, nil
}

func (c *Client) UploadHLS(ctx context.Context, localDir, orgID, mediaFileID string) error {
	prefix := fmt.Sprintf("%s/%s/%s", c.hlsPrefix, orgID, mediaFileID)

	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}

		key := prefix + "/" + filepath.ToSlash(rel)

		return c.uploadStreamingFile(ctx, path, key, contentType(path))
	})
}

func (c *Client) UploadThumbnails(ctx context.Context, localDir, orgID, mediaFileID string) error {
	prefix := fmt.Sprintf("%s/%s/%s/thumbnails", c.hlsPrefix, orgID, mediaFileID)
	files := []struct {
		name        string
		contentType string
	}{
		{name: "storyboard.jpg", contentType: "image/jpeg"},
		{name: "storyboard.vtt", contentType: "text/vtt; charset=utf-8"},
	}

	for _, file := range files {
		path := filepath.Join(localDir, file.name)
		key := prefix + "/" + file.name
		if err := c.uploadStreamingFile(ctx, path, key, file.contentType); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) UploadPoster(ctx context.Context, localPath, orgID, mediaFileID string) error {
	key := fmt.Sprintf("%s/%s/%s/thumbnail.jpg", c.hlsPrefix, orgID, mediaFileID)
	return c.uploadStreamingFile(ctx, localPath, key, "image/jpeg")
}

func (c *Client) uploadStreamingFile(ctx context.Context, path, key, fileContentType string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("upload source %s is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	_, err = c.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.streaming.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(fileContentType),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}

	c.log.Debug("uploaded", "key", key, "size", info.Size())
	return nil
}

func contentType(path string) string {
	switch filepath.Ext(path) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

func (c *Client) StreamingURL(orgID, mediaFileID string) string {
	return fmt.Sprintf("%s/%s/%s/%s/main.m3u8", c.streaming.urlPrefix, c.hlsPrefix, orgID, mediaFileID)
}
