package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	QueueKey      string
	Concurrency   int

	DatabaseURL string

	AwsRegion          string
	AwsAccessKeyID     string
	AwsSecretAccessKey string

	SourceBucket    string
	SourceEndpoint  string
	SourceURLPrefix string
	SourcePathStyle bool

	StreamingBucket    string
	StreamingEndpoint  string
	StreamingURLPrefix string
	StreamingPathStyle bool
	StreamingRegion    string

	HLSPrefix             string
	WorkDir               string
	FfmpegBin             string
	FfprobeBin            string
	FfmpegVideoCodec      string
	ProgressMinIntervalMs int
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		RedisAddr:     getenv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: normalizePassword(os.Getenv("REDIS_PASSWORD")),
		QueueKey:      getenv("QUEUE_KEY", "oxygen-database-queues:transcode"),
	}

	db, err := strconv.Atoi(getenv("REDIS_DB", "0"))
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB: %w", err)
	}
	cfg.RedisDB = db

	concurrency, err := strconv.Atoi(getenv("WORKER_CONCURRENCY", "1"))
	if err != nil || concurrency < 1 {
		return nil, fmt.Errorf("invalid WORKER_CONCURRENCY: %q", os.Getenv("WORKER_CONCURRENCY"))
	}
	cfg.Concurrency = concurrency

	cfg.DatabaseURL = getenv("DATABASE_URL", "")
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = buildPostgresDSN()
	}

	cfg.AwsRegion = getenv("AWS_DEFAULT_REGION", getenv("AWS_REGION", "us-east-1"))
	cfg.AwsAccessKeyID = getenv("AWS_ACCESS_KEY_ID", "")
	cfg.AwsSecretAccessKey = getenv("AWS_SECRET_ACCESS_KEY", "")

	cfg.SourceBucket = getenv("SOURCE_AWS_BUCKET", getenv("AWS_BUCKET", ""))
	cfg.SourceEndpoint = getenv("SOURCE_AWS_ENDPOINT", getenv("AWS_ENDPOINT", ""))
	cfg.SourceURLPrefix = getenv("SOURCE_AWS_URL", getenv("AWS_URL", ""))
	cfg.SourcePathStyle = getenv("SOURCE_AWS_USE_PATH_STYLE_ENDPOINT",
		getenv("AWS_USE_PATH_STYLE_ENDPOINT", "false")) == "true"

	cfg.StreamingBucket = getenv("STREAMING_AWS_BUCKET", getenv("AWS_BUCKET", ""))
	cfg.StreamingEndpoint = getenv("STREAMING_AWS_ENDPOINT", getenv("AWS_ENDPOINT", ""))
	cfg.StreamingURLPrefix = getenv("STREAMING_AWS_URL", getenv("AWS_URL", ""))
	cfg.StreamingPathStyle = getenv("STREAMING_AWS_USE_PATH_STYLE_ENDPOINT",
		getenv("AWS_USE_PATH_STYLE_ENDPOINT", "false")) == "true"
	cfg.StreamingRegion = getenv("STREAMING_AWS_DEFAULT_REGION", cfg.AwsRegion)

	cfg.HLSPrefix = getenv("HLS_PREFIX", "hls")
	cfg.WorkDir = getenv("WORK_DIR", "/tmp/transcoder")
	cfg.FfmpegBin = getenv("FFMPEG_BIN", "ffmpeg")
	cfg.FfprobeBin = getenv("FFPROBE_BIN", "ffprobe")
	cfg.FfmpegVideoCodec = getenv("FFMPEG_VIDEO_CODEC", "auto")

	cfg.ProgressMinIntervalMs, err = strconv.Atoi(getenv("PROGRESS_MIN_INTERVAL_MS", "2000"))
	if err != nil || cfg.ProgressMinIntervalMs < 500 {
		cfg.ProgressMinIntervalMs = 2000
	}

	return cfg, nil
}

func buildPostgresDSN() string {
	host := getenv("DB_HOST", "127.0.0.1")
	port := getenv("DB_PORT", "5432")
	dbname := getenv("DB_DATABASE", "")
	user := getenv("DB_USERNAME", "postgres")
	password := getenv("DB_PASSWORD", "")

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, password, host, port, dbname)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func normalizePassword(v string) string {
	if v == "null" {
		return ""
	}
	return v
}
