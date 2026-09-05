package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
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

func TestRestartDisconnectsPublisherWhileStreamIsStarting(t *testing.T) {
	publisher, peer := net.Pipe()
	defer peer.Close()
	srv := New(config.Config{
		ControlToken:   "secret",
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.putLiveSession(&liveSession{publicID: "public-1", conn: publisher, state: liveSessionStarting})

	req := httptest.NewRequest(http.MethodPost, "/streams/public-1/restart", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusAccepted || !strings.Contains(res.Body.String(), `"disconnected":true`) {
		t.Fatalf("unexpected restart response: %d %s", res.Code, res.Body.String())
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); err == nil {
		t.Fatal("starting publisher connection remained open")
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
	srv.putLiveSession(&liveSession{publicID: "public-1", state: liveSessionReady})

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

func TestHLSStartingStreamReturnsRetryableServiceUnavailableWithoutTracking(t *testing.T) {
	srv := New(config.Config{
		HLSRoot:        t.TempDir(),
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.putLiveSession(&liveSession{publicID: "public-1", state: liveSessionStarting})

	req := httptest.NewRequest(http.MethodGet, "/live/public-1/index.m3u8", nil)
	res := httptest.NewRecorder()
	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.Code)
	}
	if res.Header().Get("Retry-After") != "1" || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected startup headers: %v", res.Header())
	}
	if snapshots := srv.tracker.Snapshots(time.Now()); len(snapshots) != 0 {
		t.Fatalf("startup probe created viewer analytics: %#v", snapshots)
	}
}

func TestHLSMissingMediaReturnsNoStoreWithoutTracking(t *testing.T) {
	srv := New(config.Config{
		HLSRoot:        t.TempDir(),
		ViewerTTL:      45 * time.Second,
		RollupInterval: time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.putLiveSession(&liveSession{publicID: "public-1", state: liveSessionReady})

	req := httptest.NewRequest(http.MethodGet, "/live/public-1/missing.m4s", nil)
	res := httptest.NewRecorder()
	srv.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusNotFound || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected uncached 404, got %d with %v", res.Code, res.Header())
	}
	if snapshots := srv.tracker.Snapshots(time.Now()); len(snapshots) != 0 {
		t.Fatalf("missing media created viewer analytics: %#v", snapshots)
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
	srv.putLiveSession(&liveSession{publicID: "public-1", state: liveSessionReady})

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

func TestFlushSnapshotsPostsViewerRollupToLaravel(t *testing.T) {
	const sessionID = "0198d846-18e7-7f3c-b91f-ce49f2230ca1"
	minute := time.Date(2026, time.August, 9, 18, 24, 37, 0, time.UTC)

	srv := New(config.Config{
		LaravelURL:        "http://laravel.test",
		ServiceToken:      "live-token",
		ViewerTTL:         time.Minute,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", sessionID)
	srv.tracker.Observe("public-1", "viewer-1", "index.m3u8", minute.Add(-10*time.Second))

	var payload ViewerSnapshotRequest
	srv.laravel.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/internal/live/viewer-snapshot" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if r.Header.Get("X-Live-Service-Token") != "live-token" {
			t.Fatal("expected live service token header")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})}

	srv.flushSnapshots(context.Background(), minute)

	if payload.PublicID != "public-1" || payload.SessionID != sessionID {
		t.Fatalf("unexpected stream identity: %+v", payload)
	}
	if payload.Minute != "2026-08-09T18:24:00Z" {
		t.Fatalf("expected minute-aligned snapshot, got %s", payload.Minute)
	}
	if payload.CurrentViewers != 1 || payload.UniqueViewers != 1 || payload.PlaylistRequests != 1 || payload.SegmentRequests != 0 {
		t.Fatalf("unexpected viewer metrics: %+v", payload)
	}
}

func TestEndRTMPSessionEnqueuesFinalViewerSample(t *testing.T) {
	const sessionID = "0198d846-18e7-7f3c-b91f-ce49f2230ca1"
	callbackRoot := t.TempDir()
	srv := New(config.Config{
		CallbackRoot:      callbackRoot,
		ViewerTTL:         time.Minute,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", sessionID)
	srv.tracker.Observe("public-1", "viewer-1", "index.m3u8", time.Now())

	srv.endRTMPSession("public-1", sessionID)

	entries, err := os.ReadDir(callbackRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one session end callback, got %d", len(entries))
	}

	body, err := os.ReadFile(filepath.Join(callbackRoot, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	var envelope callbackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Path != "/internal/live/session-ended" {
		t.Fatalf("unexpected callback path %s", envelope.Path)
	}

	var payload struct {
		SessionID        string `json:"session_id"`
		Minute           string `json:"minute"`
		CurrentViewers   int    `json:"current_viewers"`
		PeakViewers      int    `json:"peak_viewers"`
		UniqueViewers    int    `json:"unique_viewers"`
		PlaylistRequests int64  `json:"playlist_requests"`
		SegmentRequests  int64  `json:"segment_requests"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SessionID != sessionID || payload.CurrentViewers != 1 || payload.PeakViewers != 1 || payload.UniqueViewers != 1 {
		t.Fatalf("unexpected final viewer sample: %+v", payload)
	}
	if payload.PlaylistRequests != 1 || payload.SegmentRequests != 0 {
		t.Fatalf("unexpected final viewer sample: %+v", payload)
	}

	minute, err := time.Parse(time.RFC3339, payload.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !minute.Equal(minute.Truncate(time.Minute)) {
		t.Fatalf("expected minute-aligned final sample, got %s", payload.Minute)
	}
}

func TestFailRTMPSessionEnqueuesDurableFailure(t *testing.T) {
	const sessionID = "0198d846-18e7-7f3c-b91f-ce49f2230ca1"
	callbackRoot := t.TempDir()
	srv := New(config.Config{
		CallbackRoot:      callbackRoot,
		ViewerTTL:         time.Minute,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", sessionID)

	srv.failRTMPSession("public-1", sessionID, errors.New("ffmpeg output stalled"))

	entries, err := os.ReadDir(callbackRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one failure callback, err=%v entries=%d", err, len(entries))
	}
	body, err := os.ReadFile(filepath.Join(callbackRoot, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var envelope callbackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Path != "/internal/live/session-failed" {
		t.Fatalf("unexpected callback path %s", envelope.Path)
	}
	var payload struct {
		SessionID    string `json:"session_id"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SessionID != sessionID || payload.ErrorMessage != "ffmpeg output stalled" {
		t.Fatalf("unexpected failure payload: %+v", payload)
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
	root := t.TempDir()
	streamDir := filepath.Join(root, "public-1")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(streamDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(config.Config{
		HLSRoot:           root,
		ViewerTTL:         45 * time.Second,
		RollupInterval:    time.Hour,
		MaxTrackedViewers: 100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.tracker.StartSession("public-1", "session-1")
	srv.putLiveSession(&liveSession{publicID: "public-1", state: liveSessionReady})

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

func TestTrackerAnalyticsDeltasAndIntervalPeakAreAcknowledgedAfterDurableWrite(t *testing.T) {
	tracker := NewTracker(time.Minute, 100)
	tracker.StartSession("public-1", "session-1", SessionContext{
		OrganizationID: "org-1",
		LiveStreamID:   "stream-1",
	})
	now := time.Now().UTC()
	tracker.Observe("public-1", "viewer-1", "index.m3u8", now)
	tracker.Observe("public-1", "viewer-2", "segment.mp4", now)

	prepared := tracker.PrepareAnalyticsBatch(now)
	if len(prepared) != 1 {
		t.Fatalf("expected one prepared stream, got %d", len(prepared))
	}
	if prepared[0].IdentityAdditions != 2 || prepared[0].PlaylistRequestsDelta != 1 || prepared[0].SegmentRequestsDelta != 1 {
		t.Fatalf("unexpected analytics deltas: %+v", prepared[0])
	}
	if prepared[0].IntervalPeakViewers != 2 || prepared[0].AnalyticsSequence != 2 {
		t.Fatalf("unexpected analytics peak/sequence: %+v", prepared[0])
	}

	tracker.AcknowledgeAnalyticsBatch(prepared)
	after := tracker.PrepareAnalyticsBatch(now)
	if after[0].IdentityAdditions != 0 || after[0].PlaylistRequestsDelta != 0 || after[0].SegmentRequestsDelta != 0 {
		t.Fatalf("expected acknowledged deltas to reset: %+v", after[0])
	}

	tracker.Observe("public-1", "viewer-1", "index.m3u8", now)
	next := tracker.PrepareAnalyticsBatch(now)
	if next[0].IdentityAdditions != 0 || next[0].PlaylistRequestsDelta != 1 || next[0].IntervalPeakViewers != 2 {
		t.Fatalf("expected only new interval request/peak: %+v", next[0])
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

func TestLiveTranscoderCapacityIsBounded(t *testing.T) {
	srv := New(config.Config{MaxLiveTranscoders: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if !srv.reserveTranscoder() {
		t.Fatal("expected first transcoder reservation to succeed")
	}
	if srv.reserveTranscoder() {
		t.Fatal("expected second transcoder reservation to be rejected")
	}
	srv.releaseTranscoder()
	if !srv.reserveTranscoder() {
		t.Fatal("expected transcoder reservation after release to succeed")
	}
}

func TestPrepareValidatesProductionDependencies(t *testing.T) {
	srv := New(config.Config{
		HLSRoot:        t.TempDir(),
		CallbackRoot:   t.TempDir(),
		FFmpegBin:      "true",
		ServiceToken:   "service-secret",
		ControlToken:   "control-secret",
		ViewerTTL:      time.Minute,
		RollupInterval: time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := srv.Prepare(); err != nil {
		t.Fatalf("expected valid production configuration: %v", err)
	}

	missingToken := New(config.Config{
		HLSRoot:      t.TempDir(),
		CallbackRoot: t.TempDir(),
		FFmpegBin:    "true",
		ControlToken: "control-secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := missingToken.Prepare(); err == nil || !strings.Contains(err.Error(), "LIVE_SERVICE_TOKEN") {
		t.Fatalf("expected missing service token error, got %v", err)
	}

	missingFFmpeg := New(config.Config{
		HLSRoot:      t.TempDir(),
		CallbackRoot: t.TempDir(),
		FFmpegBin:    "definitely-not-an-oxygen-binary",
		ServiceToken: "service-secret",
		ControlToken: "control-secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := missingFFmpeg.Prepare(); err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("expected missing ffmpeg error, got %v", err)
	}

	partialAnalytics := New(config.Config{
		HLSRoot:      t.TempDir(),
		CallbackRoot: t.TempDir(),
		FFmpegBin:    "true",
		ServiceToken: "service-secret",
		ControlToken: "control-secret",
		AnalyticsURL: "http://analytics.test",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := partialAnalytics.Prepare(); err == nil || !strings.Contains(err.Error(), "ANALYTICS") {
		t.Fatalf("expected partial analytics configuration error, got %v", err)
	}
}

func TestPrepareFailsOpenWhenOptionalAnalyticsOutboxIsUnavailable(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := New(config.Config{
		HLSRoot:             t.TempDir(),
		CallbackRoot:        t.TempDir(),
		FFmpegBin:           "true",
		ServiceToken:        "service-secret",
		ControlToken:        "control-secret",
		AnalyticsURL:        "http://analytics.test",
		AnalyticsToken:      "analytics-secret",
		AnalyticsOutboxRoot: filepath.Join(blockedParent, "outbox"),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := srv.Prepare(); err != nil {
		t.Fatalf("optional analytics storage must not prevent media startup: %v", err)
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

func TestCallbackOutboxQuarantinesPermanentFailure(t *testing.T) {
	client := NewLaravelClient(config.Config{LaravelURL: "http://laravel.test"})
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnprocessableEntity,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("invalid callback")),
		}, nil
	})}

	root := t.TempDir()
	outbox := NewCallbackOutbox(root, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := outbox.Enqueue("/internal/live/session-ended", map[string]string{"session_id": "invalid"}); err != nil {
		t.Fatal(err)
	}
	outbox.flush(context.Background())

	deadLetters, err := os.ReadDir(filepath.Join(root, "dead-letter"))
	if err != nil || len(deadLetters) != 1 {
		t.Fatalf("expected one dead-letter callback, err=%v entries=%d", err, len(deadLetters))
	}
}

func TestCallbackOutboxRetainsAuthenticationFailureForReplay(t *testing.T) {
	client := NewLaravelClient(config.Config{LaravelURL: "http://laravel.test"})
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("token rotation in progress")),
		}, nil
	})}

	root := t.TempDir()
	outbox := NewCallbackOutbox(root, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := outbox.Enqueue("/internal/live/session-ended", map[string]string{"session_id": "session-1"}); err != nil {
		t.Fatal(err)
	}
	outbox.flush(context.Background())

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].IsDir() || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("expected authentication failure to remain queued, got %+v", entries)
	}
}

func TestPermanentDeliveryErrorClassification(t *testing.T) {
	tests := map[int]bool{
		http.StatusBadRequest:            true,
		http.StatusRequestEntityTooLarge: true,
		http.StatusUnsupportedMediaType:  true,
		http.StatusUnprocessableEntity:   true,
		http.StatusUnauthorized:          false,
		http.StatusForbidden:             false,
		http.StatusNotFound:              false,
		http.StatusRequestTimeout:        false,
		http.StatusConflict:              false,
		http.StatusTooEarly:              false,
		http.StatusTooManyRequests:       false,
		http.StatusServiceUnavailable:    false,
	}

	for status, expected := range tests {
		t.Run(http.StatusText(status), func(t *testing.T) {
			actual := isPermanentDeliveryError(&deliveryHTTPError{status: status})
			if actual != expected {
				t.Fatalf("status %d: expected permanent=%t, got %t", status, expected, actual)
			}
		})
	}
}
