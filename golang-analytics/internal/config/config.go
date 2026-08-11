package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                    string
	DatabaseURL             string
	IngestToken             string
	QueryToken              string
	RawRetentionDays        int
	ReconciliationHours     int
	DBMaxConnections        int32
	DBMinConnections        int32
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	IdleTimeout             time.Duration
	ShutdownTimeout         time.Duration
	MaximumBatchSize        int
	MaximumRequestBodyBytes int64
}

func Load() (Config, error) {
	loadDotEnv(".env")
	loadDotEnv("../.env")

	cfg := Config{
		Addr:                    getenv("ANALYTICS_ADDR", ":8090"),
		DatabaseURL:             os.Getenv("ANALYTICS_DATABASE_URL"),
		IngestToken:             os.Getenv("ANALYTICS_INGEST_TOKEN"),
		QueryToken:              os.Getenv("ANALYTICS_QUERY_TOKEN"),
		RawRetentionDays:        intEnv("ANALYTICS_RAW_RETENTION_DAYS", 30),
		ReconciliationHours:     intEnv("ANALYTICS_RECONCILIATION_HOURS", 48),
		DBMaxConnections:        int32(intEnv("ANALYTICS_DB_MAX_CONNECTIONS", 20)),
		DBMinConnections:        int32(intEnv("ANALYTICS_DB_MIN_CONNECTIONS", 2)),
		MaximumBatchSize:        intEnv("ANALYTICS_MAXIMUM_BATCH_SIZE", 500),
		MaximumRequestBodyBytes: int64(intEnv("ANALYTICS_MAXIMUM_REQUEST_BODY_BYTES", 2*1024*1024)),
		ReadTimeout:             secondsEnv("ANALYTICS_HTTP_READ_TIMEOUT_SECONDS", 10),
		WriteTimeout:            secondsEnv("ANALYTICS_HTTP_WRITE_TIMEOUT_SECONDS", 15),
		IdleTimeout:             secondsEnv("ANALYTICS_HTTP_IDLE_TIMEOUT_SECONDS", 75),
		ShutdownTimeout:         secondsEnv("ANALYTICS_SHUTDOWN_TIMEOUT_SECONDS", 15),
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, fmt.Errorf("ANALYTICS_DATABASE_URL is required")
	}
	databaseURL, err := url.Parse(cfg.DatabaseURL)
	if err != nil || databaseURL.Host == "" || (databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") {
		return Config{}, fmt.Errorf("ANALYTICS_DATABASE_URL must be a postgres URL")
	}
	if strings.TrimSpace(cfg.IngestToken) == "" || strings.TrimSpace(cfg.QueryToken) == "" {
		return Config{}, fmt.Errorf("analytics ingest and query tokens are required")
	}
	if cfg.IngestToken == cfg.QueryToken && os.Getenv("APP_ENV") == "production" {
		return Config{}, fmt.Errorf("analytics ingest and query tokens must differ in production")
	}
	if cfg.RawRetentionDays <= 0 || cfg.ReconciliationHours <= 0 || cfg.DBMaxConnections <= 0 || cfg.DBMinConnections <= 0 || cfg.MaximumBatchSize <= 0 || cfg.MaximumRequestBodyBytes <= 0 || cfg.ReadTimeout <= 0 || cfg.WriteTimeout <= 0 || cfg.IdleTimeout <= 0 || cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("analytics limits and timeouts must be positive")
	}
	if cfg.DBMinConnections > cfg.DBMaxConnections {
		return Config{}, fmt.Errorf("minimum database connections exceed maximum")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if os.Getenv(key) == "" {
		return fallback
	}
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}

func secondsEnv(key string, fallback int) time.Duration {
	return time.Duration(intEnv(key, fallback)) * time.Second
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
	}
}
