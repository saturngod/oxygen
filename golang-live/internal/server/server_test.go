package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"oxygen/live/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

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

func TestHLSServingDoesNotTrackUnknownStreams(t *testing.T) {
	srv := New(config.Config{
		HLSRoot:           t.TempDir(),
		ViewerTTL:         45 * time.Second,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/live/unknown/index.m3u8", nil)
	res := httptest.NewRecorder()
	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	if snapshots := srv.tracker.Snapshots(time.Now()); len(snapshots) != 0 {
		t.Fatalf("expected no tracker state, got %d streams", len(snapshots))
	}
}

func TestViewerFingerprintIgnoresForwardedHeadersByDefault(t *testing.T) {
	srv := New(config.Config{
		HLSRoot:           t.TempDir(),
		ViewerTTL:         45 * time.Second,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", "session-1")

	for _, forwardedFor := range []string{"198.51.100.1", "198.51.100.2"} {
		req := httptest.NewRequest(http.MethodGet, "/live/public-1/index.m3u8", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		req.Header.Set("X-Forwarded-For", forwardedFor)
		res := httptest.NewRecorder()
		srv.Routes().ServeHTTP(res, req)
	}

	snapshots := srv.tracker.Snapshots(time.Now())
	if snapshots[0].UniqueViewers != 1 {
		t.Fatalf("expected one trusted viewer identity, got %d", snapshots[0].UniqueViewers)
	}
}

func TestTrackerCapsViewerCardinality(t *testing.T) {
	tracker := NewTracker(time.Minute, 2)
	tracker.StartSession("public-1", "session-1")

	for _, viewerID := range []string{"viewer-1", "viewer-2", "viewer-3"} {
		tracker.Observe("public-1", viewerID, "index.m3u8", time.Now())
	}

	snapshot := tracker.Snapshots(time.Now())[0]
	if snapshot.CurrentViewers != 2 {
		t.Fatalf("expected current viewers to be capped at 2, got %d", snapshot.CurrentViewers)
	}
	if snapshot.UniqueViewers != 2 {
		t.Fatalf("expected unique viewers to be capped at 2, got %d", snapshot.UniqueViewers)
	}
	if snapshot.PlaylistRequests != 3 {
		t.Fatalf("expected all requests to be counted, got %d", snapshot.PlaylistRequests)
	}
}

func TestPublisherReservationIsAtomic(t *testing.T) {
	srv := New(config.Config{
		ViewerTTL:         time.Minute,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if !srv.reservePublisher("public-1") {
		t.Fatal("expected first reservation to succeed")
	}
	if srv.reservePublisher("public-1") {
		t.Fatal("expected duplicate reservation to fail")
	}

	srv.releasePublisher("public-1")
	if !srv.reservePublisher("public-1") {
		t.Fatal("expected reservation to succeed after release")
	}
}

func TestReadinessRequiresRecoveryAndRTMPListener(t *testing.T) {
	srv := New(config.Config{
		ViewerTTL:         time.Minute,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	res := httptest.NewRecorder()
	srv.Routes().ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before startup, got %d", res.Code)
	}

	srv.recovered.Store(true)
	srv.rtmpListening.Store(true)
	srv.outboxHealthy.Store(true)
	res = httptest.NewRecorder()
	srv.Routes().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 when ready, got %d", res.Code)
	}
}

func TestCallbackOutboxPersistsAndDeliversCallbacks(t *testing.T) {
	delivered := make(chan struct{}, 1)
	client := NewLaravelClient(config.Config{LaravelURL: "http://laravel.test"})
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/internal/live/session-ended" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		delivered <- struct{}{}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	outbox := NewCallbackOutbox(t.TempDir(), client, log)
	if err := outbox.Enqueue("/internal/live/session-ended", map[string]string{"session_id": "session-1"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outbox.Run(ctx)

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("callback was not delivered")
	}
}

func TestCallbackOutboxRetainsFailedCallbacks(t *testing.T) {
	client := NewLaravelClient(config.Config{LaravelURL: "http://laravel.test"})
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("laravel unavailable")
	})}

	root := t.TempDir()
	outbox := NewCallbackOutbox(root, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := outbox.Enqueue("/internal/live/session-ended", map[string]string{"session_id": "session-1"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outbox.flush(ctx)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected failed callback to remain persisted, got %d entries", len(entries))
	}
}
