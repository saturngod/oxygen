package config

import (
	"testing"
	"time"
)

func TestLoadProductionSafetySettings(t *testing.T) {
	t.Setenv("LIVE_CALLBACK_ROOT", "/var/lib/oxygen-live/callbacks")
	t.Setenv("LIVE_TRUST_PROXY_HEADERS", "true")
	t.Setenv("MAX_TRACKED_VIEWERS", "25000")
	t.Setenv("MAX_RTMP_CONNECTIONS", "500")
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
	if cfg.ViewerTTL != time.Minute {
		t.Fatalf("expected one minute viewer ttl, got %s", cfg.ViewerTTL)
	}
}

func TestLoadRejectsUnsafeNumericValues(t *testing.T) {
	t.Setenv("MAX_TRACKED_VIEWERS", "0")
	t.Setenv("VIEWER_TTL_SECONDS", "invalid")

	cfg := Load()

	if cfg.MaxTrackedViewers != 100000 {
		t.Fatalf("expected default viewer cap, got %d", cfg.MaxTrackedViewers)
	}
	if cfg.ViewerTTL != 45*time.Second {
		t.Fatalf("expected default viewer ttl, got %s", cfg.ViewerTTL)
	}
}
