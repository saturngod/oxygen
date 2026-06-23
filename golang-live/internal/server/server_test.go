package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"oxygen/live/internal/config"
)

func TestRestartRequiresControlToken(t *testing.T) {
	srv := New(config.Config{
		ControlToken:   "secret",
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/streams/public-1/restart", nil)
	res := httptest.NewRecorder()

	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/streams/public-1/restart", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res = httptest.NewRecorder()

	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", res.Code)
	}
}

func TestSessionEndpointsRequireControlToken(t *testing.T) {
	srv := New(config.Config{
		ControlToken:   "secret",
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	endpoints := []string{"/ingest/auth", "/sessions/start", "/sessions/end", "/sessions/fail"}

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, endpoint, nil)
		res := httptest.NewRecorder()

		srv.Routes().ServeHTTP(res, req)

		if res.Code != http.StatusForbidden {
			t.Fatalf("%s without token: expected 403, got %d", endpoint, res.Code)
		}
	}
}

func TestPublishCredentialsParsesOBSStyleStreamKey(t *testing.T) {
	u, err := url.Parse("rtmp://127.0.0.1:1935/live/public-1?key=secret-key")
	if err != nil {
		t.Fatal(err)
	}

	publicID, streamKey, err := publishCredentials(u)
	if err != nil {
		t.Fatal(err)
	}

	if publicID != "public-1" {
		t.Fatalf("expected public-1, got %s", publicID)
	}
	if streamKey != "secret-key" {
		t.Fatalf("expected secret-key, got %s", streamKey)
	}
}

func TestPublishCredentialsRejectsMissingSecretKey(t *testing.T) {
	u, err := url.Parse("rtmp://127.0.0.1:1935/live/public-1")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := publishCredentials(u); err == nil {
		t.Fatal("expected error")
	}
}

func TestHLSServingTracksViewerPresence(t *testing.T) {
	root := t.TempDir()
	streamDir := filepath.Join(root, "public-1")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(streamDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(config.Config{
		HLSRoot:        root,
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", "session-1")

	req := httptest.NewRequest(http.MethodGet, "/live/public-1/index.m3u8", nil)
	res := httptest.NewRecorder()

	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	snapshots := srv.tracker.Snapshots(time.Now())
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	if snapshots[0].CurrentViewers != 1 {
		t.Fatalf("expected one current viewer, got %d", snapshots[0].CurrentViewers)
	}
	if snapshots[0].PlaylistRequests != 1 {
		t.Fatalf("expected one playlist request, got %d", snapshots[0].PlaylistRequests)
	}
}

func TestHLSServingUsesStableFingerprintWithoutCookies(t *testing.T) {
	root := t.TempDir()
	streamDir := filepath.Join(root, "public-1")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(streamDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(config.Config{
		HLSRoot:        root,
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", "session-1")

	for range 3 {
		req := httptest.NewRequest(http.MethodGet, "/live/public-1/index.m3u8", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		req.Header.Set("User-Agent", "hls-test")
		res := httptest.NewRecorder()

		srv.Routes().ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.Code)
		}
	}

	snapshots := srv.tracker.Snapshots(time.Now())
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	if snapshots[0].CurrentViewers != 1 {
		t.Fatalf("expected one current viewer, got %d", snapshots[0].CurrentViewers)
	}
	if snapshots[0].UniqueViewers != 1 {
		t.Fatalf("expected one unique viewer, got %d", snapshots[0].UniqueViewers)
	}
	if snapshots[0].PlaylistRequests != 3 {
		t.Fatalf("expected three playlist requests, got %d", snapshots[0].PlaylistRequests)
	}
}
