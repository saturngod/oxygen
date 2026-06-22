package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib/v2"

	"oxygen/live/internal/config"
)

// publicIDPattern restricts stream identifiers to filesystem- and URL-safe
// characters so they can never be used to traverse outside HLSRoot.
var publicIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validPublicID(id string) bool {
	return publicIDPattern.MatchString(id)
}

type Server struct {
	cfg     config.Config
	log     *slog.Logger
	laravel *LaravelClient
	tracker *Tracker

	mu      sync.RWMutex
	streams map[string]*liveSession
}

type liveSession struct {
	publicID  string
	sessionID string
	muxer     *gohlslib.Muxer
	conn      net.Conn

	mu     sync.RWMutex
	closed bool
}

// handle serves an HLS request from the live muxer. It returns false if the
// session has already been torn down, so the caller can fall back to disk.
// The read lock is held for the duration of the muxer call, which is what
// makes it safe against a concurrent close() / muxer.Close().
func (ls *liveSession) handle(w http.ResponseWriter, r *http.Request) bool {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	if ls.closed {
		return false
	}

	ls.muxer.Handle(w, r)

	return true
}

// close shuts the muxer down exactly once, blocking until no viewer request is
// mid-flight (via the same RWMutex handle() uses).
func (ls *liveSession) close() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.closed {
		return
	}

	ls.closed = true
	ls.muxer.Close()
}

// disconnect closes the underlying RTMP connection, which unblocks the read
// loop and drives the normal teardown path.
func (ls *liveSession) disconnect() {
	if ls.conn != nil {
		_ = ls.conn.Close()
	}
}

func New(cfg config.Config, log *slog.Logger) *Server {
	return &Server{
		cfg:     cfg,
		log:     log,
		laravel: NewLaravelClient(cfg),
		tracker: NewTracker(cfg.ViewerTTL),
		streams: make(map[string]*liveSession),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
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

func (s *Server) RecoverActiveSessions(ctx context.Context) {
	err := s.laravel.Post(ctx, "/internal/live/recover-active", map[string]bool{"ok": true}, nil)
	if err != nil {
		s.log.Warn("active session recovery failed", "err", err)
		return
	}

	s.log.Info("active live sessions recovered")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authPublish(w http.ResponseWriter, r *http.Request) {
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
		http.NotFound(w, r)
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, "/live/"+publicID+"/")
	clean := filepath.Clean(rel)

	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		http.NotFound(w, r)
		return
	}

	viewerID := s.viewerID(w, r)
	s.tracker.Observe(publicID, viewerID, clean, time.Now())

	// The gohlslib muxer sets CDN-friendly per-file Cache-Control headers itself
	// (no-cache for the low-latency playlist, public max-age for segments), so we
	// pass the writer through untouched and let it manage caching.
	if session := s.getLiveSession(publicID); session != nil {
		if session.handle(w, r) {
			return
		}
	}

	streamRoot := filepath.Join(s.cfg.HLSRoot, publicID)
	path := filepath.Join(streamRoot, clean)

	// Defence-in-depth: ensure the resolved path is still contained in the
	// stream's directory even if clean/publicID combined in an unexpected way.
	if path != streamRoot && !strings.HasPrefix(path, streamRoot+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}

	// Go's http.ServeFile sets no Cache-Control, so set one by file type for the
	// disk fallback: playlists must always revalidate (live), segments are
	// uniquely named per muxer run and may be cached indefinitely by a CDN.
	setHLSCacheHeader(w, clean)
	http.ServeFile(w, r, path)
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

func (s *Server) removeLiveSession(publicID string, session *liveSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streams[publicID] == session {
		delete(s.streams, publicID)
	}
}

func (s *Server) flushSnapshots(ctx context.Context, now time.Time) {
	minute := now.UTC().Truncate(time.Minute).Format(time.RFC3339)

	for _, snapshot := range s.tracker.Snapshots(now) {
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

	fingerprint := viewerFingerprint(r)
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

func viewerFingerprint(r *http.Request) string {
	host := r.Header.Get("X-Forwarded-For")
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
