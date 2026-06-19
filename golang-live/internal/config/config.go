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
	LaravelURL           string
	ServiceToken         string
	ControlToken         string
	AllowInsecureControl bool
	ViewerTTL            time.Duration
	RollupInterval       time.Duration
}

func Load() Config {
	loadDotEnv(".env")
	loadDotEnv("../.env")

	return Config{
		Addr:                 getenv("LIVE_ADDR", ":8081"),
		RTMPAddr:             getenv("LIVE_RTMP_ADDR", ":1935"),
		HLSRoot:              getenv("LIVE_HLS_ROOT", "/tmp/oxygen-live/hls"),
		LaravelURL:           strings.TrimRight(getenv("LARAVEL_URL", "http://127.0.0.1:8000"), "/"),
		ServiceToken:         getenv("LIVE_SERVICE_TOKEN", ""),
		ControlToken:         getenv("LIVE_CONTROL_TOKEN", ""),
		AllowInsecureControl: boolEnv("LIVE_ALLOW_INSECURE_CONTROL", false),
		ViewerTTL:            secondsEnv("VIEWER_TTL_SECONDS", 45),
		RollupInterval:       secondsEnv("ROLLUP_INTERVAL_SECONDS", 15),
	}
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
