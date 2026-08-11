package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                 string
	RTMPAddr             string
	HLSRoot              string
	FFmpegBin            string
	FFmpegVideoCodec     string
	CallbackRoot         string
	AnalyticsURL         string
	AnalyticsToken       string
	AnalyticsOutboxRoot  string
	AnalyticsBatchSize   int
	LaravelURL           string
	ServiceToken         string
	ControlToken         string
	AllowInsecureControl bool
	TrustProxyHeaders    bool
	ViewerTTL            time.Duration
	RollupInterval       time.Duration
	MaxTrackedViewers    int
	MaxRTMPConnections   int
}

func Load() Config {
	loadDotEnv(".env")
	loadDotEnv("../.env")

	return Config{
		Addr:                 getenv("LIVE_ADDR", ":8081"),
		RTMPAddr:             getenv("LIVE_RTMP_ADDR", ":1935"),
		HLSRoot:              getenv("LIVE_HLS_ROOT", "/tmp/oxygen-live/hls"),
		FFmpegBin:            getenv("FFMPEG_BIN", "ffmpeg"),
		FFmpegVideoCodec:     getenv("FFMPEG_VIDEO_CODEC", "libx264"),
		CallbackRoot:         getenv("LIVE_CALLBACK_ROOT", "/tmp/oxygen-live/callbacks"),
		AnalyticsURL:         strings.TrimRight(getenv("ANALYTICS_URL", ""), "/"),
		AnalyticsToken:       getenv("ANALYTICS_INGEST_TOKEN", ""),
		AnalyticsOutboxRoot:  getenv("ANALYTICS_OUTBOX_ROOT", "/tmp/oxygen-live/analytics-outbox"),
		AnalyticsBatchSize:   intEnv("ANALYTICS_BATCH_SIZE", 100),
		LaravelURL:           strings.TrimRight(getenv("LARAVEL_URL", "http://127.0.0.1:8000"), "/"),
		ServiceToken:         getenv("LIVE_SERVICE_TOKEN", ""),
		ControlToken:         getenv("LIVE_CONTROL_TOKEN", ""),
		AllowInsecureControl: boolEnv("LIVE_ALLOW_INSECURE_CONTROL", false),
		TrustProxyHeaders:    boolEnv("LIVE_TRUST_PROXY_HEADERS", false),
		ViewerTTL:            secondsEnv("VIEWER_TTL_SECONDS", 45),
		RollupInterval:       secondsEnv("ROLLUP_INTERVAL_SECONDS", 15),
		MaxTrackedViewers:    intEnv("MAX_TRACKED_VIEWERS", 100000),
		MaxRTMPConnections:   intEnv("MAX_RTMP_CONNECTIONS", 1000),
	}
}

func intEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}

	return n
}

func boolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func secondsEnv(key string, fallback int) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return time.Duration(fallback) * time.Second
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return time.Duration(fallback) * time.Second
	}

	return time.Duration(n) * time.Second
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

		os.Setenv(key, cleanEnvValue(value))
	}
}

func cleanEnvValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)

	return value
}
