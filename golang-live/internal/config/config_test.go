package config

import (
	"testing"
	"time"
)

func TestLoadDefaultStallTimeout(t *testing.T) {
	for _, value := range []string{"", "invalid", "0", "-1", "10"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("FFMPEG_OUTPUT_STALL_TIMEOUT_SECONDS", value)
			if got := Load().FFmpegStallTimeout; got != 10*time.Second {
				t.Fatalf("expected 10 second stall timeout, got %s", got)
			}
		})
	}
}

func TestLoadProductionSafetySettings(t *testing.T) {
	t.Setenv("LIVE_CALLBACK_ROOT", "/var/lib/oxygen-live/callbacks")
	t.Setenv("LIVE_TRUST_PROXY_HEADERS", "true")
	t.Setenv("MAX_TRACKED_VIEWERS", "25000")
	t.Setenv("MAX_RTMP_CONNECTIONS", "500")
	t.Setenv("MAX_LIVE_TRANSCODERS", "4")
	t.Setenv("FFMPEG_RELAY_WRITE_TIMEOUT_SECONDS", "12")
	t.Setenv("FFMPEG_OUTPUT_STALL_TIMEOUT_SECONDS", "40")
	t.Setenv("VIEWER_TTL_SECONDS", "60")

	cfg := Load()

	if cfg.CallbackRoot != "/var/lib/oxygen-live/callbacks" {
		t.Fatalf("unexpected callback root %s", cfg.CallbackRoot)
	}
	if !cfg.TrustProxyHeaders {
		t.Fatal("expected proxy headers to be trusted")
	}
	if cfg.MaxTrackedViewers != 25000 {
		t.Fatalf("expected viewer cap 25000, got %d", cfg.MaxTrackedViewers)
	}
	if cfg.MaxRTMPConnections != 500 {
		t.Fatalf("expected RTMP connection cap 500, got %d", cfg.MaxRTMPConnections)
	}
	if cfg.MaxLiveTranscoders != 4 {
		t.Fatalf("expected live transcoder cap 4, got %d", cfg.MaxLiveTranscoders)
	}
	if cfg.FFmpegWriteTimeout != 12*time.Second {
		t.Fatalf("expected 12 second ffmpeg write timeout, got %s", cfg.FFmpegWriteTimeout)
	}
	if cfg.FFmpegStallTimeout != 40*time.Second {
		t.Fatalf("expected 40 second ffmpeg stall timeout, got %s", cfg.FFmpegStallTimeout)
	}
	if cfg.ViewerTTL != time.Minute {
		t.Fatalf("expected one minute viewer ttl, got %s", cfg.ViewerTTL)
	}
}

func TestLoadRejectsUnsafeNumericValues(t *testing.T) {
	t.Setenv("MAX_TRACKED_VIEWERS", "0")
	t.Setenv("VIEWER_TTL_SECONDS", "invalid")
	t.Setenv("MAX_LIVE_TRANSCODERS", "0")
	t.Setenv("FFMPEG_RELAY_WRITE_TIMEOUT_SECONDS", "invalid")

	cfg := Load()

	if cfg.MaxTrackedViewers != 100000 {
		t.Fatalf("expected default viewer cap, got %d", cfg.MaxTrackedViewers)
	}
	if cfg.ViewerTTL != 45*time.Second {
		t.Fatalf("expected default viewer ttl, got %s", cfg.ViewerTTL)
	}
	if cfg.MaxLiveTranscoders != 2 {
		t.Fatalf("expected default live transcoder cap, got %d", cfg.MaxLiveTranscoders)
	}
	if cfg.FFmpegWriteTimeout != 10*time.Second {
		t.Fatalf("expected default ffmpeg write timeout, got %s", cfg.FFmpegWriteTimeout)
	}
}
