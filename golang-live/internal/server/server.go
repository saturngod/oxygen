package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gohlslib/v2"
	"github.com/google/uuid"

	"oxygen/live/internal/config"
)

// publicIDPattern restricts stream identifiers to filesystem- and URL-safe
// characters so they can never be used to traverse outside HLSRoot.
var publicIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validPublicID(id string) bool {
	return publicIDPattern.MatchString(id)
}

type Server struct {
	cfg             config.Config
	log             *slog.Logger
	laravel         *LaravelClient
	tracker         *Tracker
	outbox          *CallbackOutbox
	analyticsOutbox *AnalyticsOutbox

	mu         sync.RWMutex
	streams    map[string]*liveSession
	publishers map[string]struct{}

	rtmpMu         sync.Mutex
	rtmpConns      map[net.Conn]struct{}
	rtmpWG         sync.WaitGroup
	rtmpSlots      chan struct{}
	transcodeSlots chan struct{}

	recovered     atomic.Bool
	rtmpListening atomic.Bool
	outboxHealthy atomic.Bool
}

type liveSession struct {
	publicID  string
	sessionID string
	muxer     *gohlslib.Muxer
	conn      net.Conn
	closeFn   func()

	mu    sync.RWMutex
	state liveSessionState
}

type liveSessionState uint8

const (
	liveSessionStarting liveSessionState = iota
	liveSessionReady
	liveSessionClosing
)

func (ls *liveSession) currentState() liveSessionState {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	return ls.state
}

func (ls *liveSession) markReady() bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.state != liveSessionStarting {
		return false
	}
	ls.state = liveSessionReady

	return true
}

func (ls *liveSession) markClosing() {
	ls.mu.Lock()
	ls.state = liveSessionClosing
	ls.mu.Unlock()
}

// close shuts down the media output after the reader has drained.
func (ls *liveSession) close() {
	ls.markClosing()
	if ls.muxer != nil {
		ls.muxer.Close()
	}
	if ls.closeFn != nil {
		ls.closeFn()
	}
}

// disconnect closes the underlying RTMP connection, which unblocks the read
// loop and drives the normal teardown path.
func (ls *liveSession) disconnect() {
	if ls.conn != nil {
		_ = ls.conn.Close()
	}
}

func New(cfg config.Config, log *slog.Logger) *Server {
	if cfg.MaxTrackedViewers <= 0 {
		cfg.MaxTrackedViewers = 100000
	}
	if cfg.MaxRTMPConnections <= 0 {
		cfg.MaxRTMPConnections = 1000
	}
	if cfg.MaxLiveTranscoders <= 0 {
		cfg.MaxLiveTranscoders = 2
	}
	if cfg.FFmpegWriteTimeout <= 0 {
		cfg.FFmpegWriteTimeout = 10 * time.Second
	}
	if cfg.FFmpegStallTimeout <= 0 {
		cfg.FFmpegStallTimeout = 10 * time.Second
	}
	if cfg.HLSStartupTimeout <= 0 {
		cfg.HLSStartupTimeout = 30 * time.Second
	}
	if cfg.FFmpegAnalyzeDuration <= 0 {
		cfg.FFmpegAnalyzeDuration = 1000000
	}
	if cfg.FFmpegProbeSize <= 0 {
		cfg.FFmpegProbeSize = 1048576
	}
	laravel := NewLaravelClient(cfg)
	var analyticsOutbox *AnalyticsOutbox
	if cfg.AnalyticsURL != "" && cfg.AnalyticsToken != "" {
		analyticsOutbox = NewAnalyticsOutbox(cfg.AnalyticsOutboxRoot, NewAnalyticsClient(cfg), log)
	}

	return &Server{
		cfg:             cfg,
		log:             log,
		laravel:         laravel,
		tracker:         NewTracker(cfg.ViewerTTL, cfg.MaxTrackedViewers),
		outbox:          NewCallbackOutbox(cfg.CallbackRoot, laravel, log),
		analyticsOutbox: analyticsOutbox,
		streams:         make(map[string]*liveSession),
		publishers:      make(map[string]struct{}),
		rtmpConns:       make(map[net.Conn]struct{}),
		rtmpSlots:       make(chan struct{}, cfg.MaxRTMPConnections),
		transcodeSlots:  make(chan struct{}, cfg.MaxLiveTranscoders),
	}
}

func (s *Server) Prepare() error {
	if strings.TrimSpace(s.cfg.ServiceToken) == "" {
		return fmt.Errorf("LIVE_SERVICE_TOKEN must not be empty")
	}
	if strings.TrimSpace(s.cfg.ControlToken) == "" && !s.cfg.AllowInsecureControl {
		return fmt.Errorf("LIVE_CONTROL_TOKEN must not be empty unless LIVE_ALLOW_INSECURE_CONTROL is enabled")
	}
	if strings.TrimSpace(s.cfg.HLSRoot) == "" {
		return fmt.Errorf("LIVE_HLS_ROOT must not be empty")
	}
	if (s.cfg.AnalyticsURL == "") != (s.cfg.AnalyticsToken == "") {
		return fmt.Errorf("ANALYTICS_URL and ANALYTICS_INGEST_TOKEN must be configured together")
	}
	if _, err := exec.LookPath(s.cfg.FFmpegBin); err != nil {
		return fmt.Errorf("find ffmpeg binary %q: %w", s.cfg.FFmpegBin, err)
	}
	if err := os.MkdirAll(s.cfg.HLSRoot, 0o755); err != nil {
		return fmt.Errorf("create HLS root: %w", err)
	}
	probe, err := os.CreateTemp(s.cfg.HLSRoot, ".oxygen-write-check-*")
	if err != nil {
		return fmt.Errorf("HLS root is not writable: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("close HLS root write check: %w", err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("remove HLS root write check: %w", err)
	}

	if err := s.outbox.Prepare(); err != nil {
		return err
	}

	s.outboxHealthy.Store(true)
	if s.analyticsOutbox != nil {
		if err := s.analyticsOutbox.Prepare(); err != nil {
			s.log.Error("analytics outbox preparation failed; analytics delivery is disabled until storage recovers", "err", err)
		}
	}

	return nil
}

func (s *Server) reserveTranscoder() bool {
	select {
	case s.transcodeSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseTranscoder() {
	<-s.transcodeSlots
}

func (s *Server) RunCallbacks(ctx context.Context) {
	s.outbox.Run(ctx)
}

func (s *Server) RunAnalyticsOutbox(ctx context.Context) {
	if s.analyticsOutbox != nil {
		s.analyticsOutbox.Run(ctx)
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("POST /ingest/auth", s.authPublish)
	mux.HandleFunc("POST /sessions/start", s.sessionStart)
	mux.HandleFunc("POST /sessions/end", s.sessionEnd)
	mux.HandleFunc("POST /sessions/fail", s.sessionFail)
	mux.HandleFunc("POST /streams/{publicID}/restart", s.restart)
	mux.HandleFunc("GET /live/{publicID}/", s.hls)

	return mux
}

func (s *Server) RunRollups(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.RollupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.flushSnapshots(ctx, now)
		}
	}
}

func (s *Server) RecoverActiveSessions(ctx context.Context) bool {
	backoff := time.Second

	for {
		err := s.laravel.Post(ctx, "/internal/live/recover-active", map[string]bool{"ok": true}, nil)
		if err == nil {
			s.recovered.Store(true)
			s.log.Info("active live sessions recovered")
			return true
		}

		s.log.Warn("active session recovery failed", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}

		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ready := s.recovered.Load() && s.rtmpListening.Load() && s.outboxHealthy.Load()
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, map[string]bool{
		"ok":             ready,
		"recovered":      s.recovered.Load(),
		"rtmp_listening": s.rtmpListening.Load(),
		"outbox_healthy": s.outboxHealthy.Load(),
	})
}

func (s *Server) authPublish(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}

	var req AuthPublishRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var resp AuthPublishResponse
	if err := s.laravel.Post(r.Context(), "/internal/live/auth-publish", req, &resp); err != nil {
		s.log.Warn("publish auth rejected", "err", err, "public_id", req.PublicID)
		writeJSON(w, http.StatusForbidden, map[string]any{"allowed": false})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) sessionStart(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}

	var req SessionStartedRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var resp SessionStartedResponse
	if err := s.laravel.Post(r.Context(), "/internal/live/session-started", req, &resp); err != nil {
		s.log.Error("session start callback failed", "err", err, "public_id", req.PublicID)
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false})
		return
	}

	if resp.SessionID != "" {
		s.tracker.StartSession(req.PublicID, resp.SessionID)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) sessionEnd(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}

	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}

	publicID, _ := payload["public_id"].(string)
	if err := s.laravel.Post(r.Context(), "/internal/live/session-ended", payload, nil); err != nil {
		s.log.Error("session end callback failed", "err", err, "public_id", publicID)
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false})
		return
	}

	if publicID != "" {
		s.tracker.EndSession(publicID)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) sessionFail(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}

	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}

	publicID, _ := payload["public_id"].(string)
	if err := s.laravel.Post(r.Context(), "/internal/live/session-failed", payload, nil); err != nil {
		s.log.Error("session fail callback failed", "err", err, "public_id", publicID)
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false})
		return
	}

	if publicID != "" {
		s.tracker.EndSession(publicID)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) restart(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}

	publicID := r.PathValue("publicID")

	// Disconnect the active publisher (if any) so the rotated key/new settings
	// take effect immediately. Closing the connection unblocks the RTMP read
	// loop, which runs the normal session teardown and end callbacks.
	disconnected := false
	if session := s.getLiveSession(publicID); session != nil {
		session.disconnect()
		disconnected = true
	}

	s.log.Info("restart requested", "public_id", publicID, "disconnected", disconnected)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":           true,
		"public_id":    publicID,
		"disconnected": disconnected,
	})
}

func (s *Server) hls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range, Origin, Accept")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	publicID := r.PathValue("publicID")
	if !validPublicID(publicID) {
		writeHLSNotFound(w, r)
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, "/live/"+publicID+"/")
	clean := filepath.Clean(rel)

	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		writeHLSNotFound(w, r)
		return
	}

	session := s.getLiveSession(publicID)
	if session == nil {
		writeHLSNotFound(w, r)
		return
	}
	switch session.currentState() {
	case liveSessionStarting:
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "1")
		http.Error(w, "live stream is starting", http.StatusServiceUnavailable)
		return
	case liveSessionClosing:
		writeHLSNotFound(w, r)
		return
	}

	streamRoot := filepath.Join(s.cfg.HLSRoot, publicID)
	filePath, ok := containedHLSPath(streamRoot, clean)
	if !ok {
		writeHLSNotFound(w, r)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		writeHLSNotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		writeHLSNotFound(w, r)
		return
	}

	viewerID := s.viewerID(w, r)
	s.tracker.Observe(publicID, viewerID, clean, time.Now())
	setHLSCacheHeader(w, clean)
	http.ServeContent(w, r, filepath.Base(clean), info.ModTime(), file)
}

func containedHLSPath(root string, relative string) (string, bool) {
	resolved := filepath.Join(root, filepath.Clean(relative))
	return resolved, resolved != root && strings.HasPrefix(resolved, root+string(os.PathSeparator))
}

func writeHLSNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.NotFound(w, r)
}

// setHLSCacheHeader applies a CDN-friendly Cache-Control header based on the
// requested file. The .m3u8 playlist changes every few seconds and must never
// be cached stale; segments (.mp4 init/seg/part) have immutable, unique names.
func setHLSCacheHeader(w http.ResponseWriter, name string) {
	if strings.HasSuffix(name, ".m3u8") {
		w.Header().Set("Cache-Control", "no-cache")
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
}

func (s *Server) getLiveSession(publicID string) *liveSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.streams[publicID]
}

func (s *Server) putLiveSession(session *liveSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streams[session.publicID] != nil {
		return false
	}

	s.streams[session.publicID] = session

	return true
}

func (s *Server) reservePublisher(publicID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.publishers[publicID]; exists {
		return false
	}

	s.publishers[publicID] = struct{}{}

	return true
}

func (s *Server) releasePublisher(publicID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.publishers, publicID)
}

func (s *Server) removeLiveSession(publicID string, session *liveSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streams[publicID] == session {
		delete(s.streams, publicID)
	}
}

func (s *Server) flushSnapshots(ctx context.Context, now time.Time) {
	minute := now.UTC().Truncate(time.Minute).Format(time.RFC3339)
	snapshots := s.tracker.Snapshots(now)
	if s.analyticsOutbox == nil {
		for _, snapshot := range snapshots {
			if snapshot.SessionID == "" {
				continue
			}

			err := s.laravel.Post(ctx, "/internal/live/viewer-snapshot", ViewerSnapshotRequest{
				PublicID:         snapshot.PublicID,
				SessionID:        snapshot.SessionID,
				Minute:           minute,
				CurrentViewers:   snapshot.CurrentViewers,
				UniqueViewers:    snapshot.UniqueViewers,
				PlaylistRequests: snapshot.PlaylistRequests,
				SegmentRequests:  snapshot.SegmentRequests,
			}, nil)
			if err != nil {
				s.log.Warn("viewer snapshot failed", "err", err, "public_id", snapshot.PublicID)
			}
		}
	}

	if s.analyticsOutbox == nil {
		return
	}
	prepared := s.tracker.PrepareAnalyticsBatch(now)
	for offset := 0; offset < len(prepared); offset += maxInt(1, s.cfg.AnalyticsBatchSize) {
		end := offset + maxInt(1, s.cfg.AnalyticsBatchSize)
		if end > len(prepared) {
			end = len(prepared)
		}
		batchSnapshots := prepared[offset:end]
		events := make([]AnalyticsEvent, 0, len(batchSnapshots))
		for _, snapshot := range batchSnapshots {
			if snapshot.SessionID == "" || snapshot.OrganizationID == "" || snapshot.LiveStreamID == "" {
				continue
			}
			events = append(events, analyticsEventFromSnapshot(snapshot, AnalyticsViewerSample, now, nil))
		}
		if len(events) == 0 {
			continue
		}
		if err := s.analyticsOutbox.Enqueue(AnalyticsEventBatch{Events: events}); err != nil {
			s.log.Error("analytics sample could not be persisted", "err", err)
			continue
		}
		s.tracker.AcknowledgeAnalyticsBatch(batchSnapshots)
	}
}

func analyticsEventFromSnapshot(snapshot Snapshot, eventType AnalyticsEventType, occurredAt time.Time, endedAt *time.Time) AnalyticsEvent {
	return AnalyticsEvent{
		EventID:           uuid.NewString(),
		EventType:         eventType,
		SchemaVersion:     1,
		Sequence:          maxInt64(1, snapshot.AnalyticsSequence),
		OccurredAt:        occurredAt.UTC(),
		OrganizationID:    snapshot.OrganizationID,
		LiveStreamID:      snapshot.LiveStreamID,
		SessionID:         snapshot.SessionID,
		CurrentViewers:    snapshot.CurrentViewers,
		IntervalPeak:      snapshot.IntervalPeakViewers,
		SessionPeak:       snapshot.PeakViewers,
		IdentityAdditions: snapshot.IdentityAdditions,
		PlaylistDelta:     snapshot.PlaylistRequestsDelta,
		SegmentDelta:      snapshot.SegmentRequestsDelta,
		UniqueTotal:       int64(snapshot.UniqueViewers),
		PlaylistTotal:     snapshot.PlaylistRequests,
		SegmentTotal:      snapshot.SegmentRequests,
		EndedAt:           endedAt,
	}
}

func maxInt(value, fallback int) int {
	if value > fallback {
		return value
	}
	return fallback
}

func maxInt64(value, fallback int64) int64 {
	if value > fallback {
		return value
	}
	return fallback
}

func (s *Server) authorizeControl(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.ControlToken == "" {
		// Fail closed by default: an unset control token must not silently
		// expose the control plane. Opt in to insecure mode explicitly for dev.
		if s.cfg.AllowInsecureControl {
			return true
		}

		s.log.Warn("control request denied: LIVE_CONTROL_TOKEN is not set")
		http.Error(w, "control token not configured", http.StatusForbidden)
		return false
	}

	expected := []byte("Bearer " + s.cfg.ControlToken)
	provided := []byte(r.Header.Get("Authorization"))

	if subtle.ConstantTimeCompare(expected, provided) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	return true
}

func (s *Server) viewerID(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie("oxygen_live_viewer")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	fingerprint := viewerFingerprint(r, s.cfg.TrustProxyHeaders)
	if fingerprint != "" {
		return fingerprint
	}

	id := randomHex(16)
	http.SetCookie(w, &http.Cookie{
		Name:     "oxygen_live_viewer",
		Value:    id,
		Path:     "/live/",
		MaxAge:   60 * 60 * 24,
		SameSite: http.SameSiteLaxMode,
	})

	return id
}

func viewerFingerprint(r *http.Request, trustProxyHeaders bool) string {
	host := ""
	if trustProxyHeaders {
		host = r.Header.Get("X-Forwarded-For")
	}
	if host == "" {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if host == "" {
		return ""
	}

	if index := strings.Index(host, ","); index >= 0 {
		host = strings.TrimSpace(host[:index])
	}

	sum := sha256.Sum256([]byte(host + "|" + r.UserAgent()))

	return hex.EncodeToString(sum[:16])
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()

	// Bound the request body so a malicious or buggy caller cannot stream an
	// unbounded payload into the control plane.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
