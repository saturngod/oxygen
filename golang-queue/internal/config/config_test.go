package config

import (
	"strings"
	"testing"
)

var thumbnailEnvironmentKeys = []string{
	"THUMBNAIL_INTERVAL_SECONDS",
	"THUMBNAIL_WIDTH",
	"THUMBNAIL_COLUMNS",
	"THUMBNAIL_ROWS",
	"THUMBNAIL_JPEG_QUALITY",
	"THUMBNAIL_POSTER_WIDTH",
}

func clearThumbnailEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range thumbnailEnvironmentKeys {
		t.Setenv(key, "")
	}
}

func TestLoadThumbnailDefaults(t *testing.T) {
	clearThumbnailEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ThumbnailIntervalSeconds != 10 || cfg.ThumbnailWidth != 160 || cfg.ThumbnailColumns != 10 || cfg.ThumbnailRows != 10 || cfg.ThumbnailJPEGQuality != 5 || cfg.ThumbnailPosterWidth != 960 {
		t.Fatalf("unexpected thumbnail defaults: %+v", cfg)
	}
}

func TestLoadRejectsInvalidThumbnailConfig(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "THUMBNAIL_INTERVAL_SECONDS", value: "not-a-number"},
		{key: "THUMBNAIL_INTERVAL_SECONDS", value: "NaN"},
		{key: "THUMBNAIL_WIDTH", value: "31"},
		{key: "THUMBNAIL_COLUMNS", value: "0"},
		{key: "THUMBNAIL_ROWS", value: "21"},
		{key: "THUMBNAIL_JPEG_QUALITY", value: "1"},
		{key: "THUMBNAIL_POSTER_WIDTH", value: "8193"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			clearThumbnailEnvironment(t)
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("Load() error = %v, want error mentioning %s", err, tt.key)
			}
		})
	}
}

func TestLoadRejectsExcessiveStoryboardDimensions(t *testing.T) {
	clearThumbnailEnvironment(t)
	t.Setenv("THUMBNAIL_WIDTH", "1920")
	t.Setenv("THUMBNAIL_COLUMNS", "20")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "storyboard width") {
		t.Fatalf("Load() error = %v", err)
	}
}
