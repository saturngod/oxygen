package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib/v2"
	hlscodecs "github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/gortmplib"
	rtmpcodecs "github.com/bluenviron/gortmplib/pkg/codecs"
	"github.com/google/uuid"
)

func (s *Server) RunRTMP(ctx context.Context) error {
	if s.cfg.RTMPAddr == "" {
		return fmt.Errorf("LIVE_RTMP_ADDR must not be empty")
	}

	ln, err := net.Listen("tcp", s.cfg.RTMPAddr)
	if err != nil {
		return fmt.Errorf("listen for rtmp on %s: %w", s.cfg.RTMPAddr, err)
	}
	defer ln.Close()
	s.rtmpListening.Store(true)
	defer s.rtmpListening.Store(false)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		s.closeRTMPConnections()
	}()

	s.log.Info("rtmp ingest listening", "addr", s.cfg.RTMPAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}

			s.log.Warn("rtmp accept failed", "err", err)
			continue
		}

		select {
		case s.rtmpSlots <- struct{}{}:
		default:
			s.log.Warn("rtmp connection limit reached", "remote_addr", conn.RemoteAddr().String())
			_ = conn.Close()
			continue
		}

		s.rtmpMu.Lock()
		if ctx.Err() != nil {
			s.rtmpMu.Unlock()
			<-s.rtmpSlots
			_ = conn.Close()
			continue
		}
		s.rtmpConns[conn] = struct{}{}
		s.rtmpWG.Add(1)
		s.rtmpMu.Unlock()

		go func() {
			defer s.rtmpWG.Done()
			defer func() { <-s.rtmpSlots }()
			defer func() {
				s.rtmpMu.Lock()
				delete(s.rtmpConns, conn)
				s.rtmpMu.Unlock()
			}()

			s.handleRTMPConn(ctx, conn)
		}()
	}
}

func (s *Server) WaitForRTMPConnections() {
	s.rtmpWG.Wait()
}

func (s *Server) closeRTMPConnections() {
	s.rtmpMu.Lock()
	defer s.rtmpMu.Unlock()

	for conn := range s.rtmpConns {
		_ = conn.Close()
	}
}

func (s *Server) handleRTMPConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	s.log.Info("rtmp connection opened", "remote_addr", remoteAddr)

	if err := s.handleRTMPConnInner(ctx, conn); err != nil {
		s.log.Warn("rtmp connection closed", "remote_addr", remoteAddr, "err", err)
		return
	}

	s.log.Info("rtmp connection closed", "remote_addr", remoteAddr)
}

func (s *Server) handleRTMPConnInner(ctx context.Context, conn net.Conn) error {
	connectionStartedAt := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	rtmpConn := &gortmplib.ServerConn{RW: conn}
	if err := rtmpConn.Initialize(); err != nil {
		return fmt.Errorf("initialize rtmp: %w", err)
	}

	if err := rtmpConn.Accept(); err != nil {
		return fmt.Errorf("accept rtmp: %w", err)
	}

	if !rtmpConn.Publish {
		return fmt.Errorf("playback over rtmp is not supported")
	}

	publicID, streamKey, err := publishCredentials(rtmpConn.URL)
	if err != nil {
		return err
	}

	var auth AuthPublishResponse
	if err := s.laravel.Post(ctx, "/internal/live/auth-publish", AuthPublishRequest{
		PublicID:  publicID,
		StreamKey: streamKey,
	}, &auth); err != nil {
		return fmt.Errorf("publish auth failed: %w", err)
	}

	if !auth.Allowed {
		return fmt.Errorf("publish auth rejected")
	}
	s.log.Info("rtmp publisher authenticated", "public_id", publicID, "elapsed", time.Since(connectionStartedAt))

	if !s.reservePublisher(publicID) {
		return fmt.Errorf("stream %s is already active", publicID)
	}
	defer s.releasePublisher(publicID)
	startupStartedAt := time.Now()
	startupCtx, cancelStartup := context.WithTimeout(ctx, s.cfg.HLSStartupTimeout)
	defer cancelStartup()

	_ = conn.SetReadDeadline(time.Now().Add(s.cfg.HLSStartupTimeout))

	reader := &gortmplib.Reader{Conn: rtmpConn}
	if err := reader.Initialize(); err != nil {
		return fmt.Errorf("initialize rtmp reader: %w", err)
	}
	s.log.Info("rtmp tracks discovered", "public_id", publicID, "elapsed", time.Since(startupStartedAt), "track_count", len(reader.Tracks()))

	hlsDir := filepath.Join(s.cfg.HLSRoot, publicID)
	// A hard process stop can leave a previous run's files behind. Each public ID
	// has at most one reserved publisher, so it is safe to reset its workspace
	// before creating the new session.
	s.cleanupHLSDir(hlsDir)
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		return fmt.Errorf("create hls dir: %w", err)
	}
	defer s.cleanupHLSDir(hlsDir)

	session := &liveSession{
		publicID: publicID,
		conn:     conn,
		state:    liveSessionStarting,
	}
	directFailure := &sessionFailure{}
	stats := newRelayStats(startupStartedAt)
	reportDirectFailure := func(err error) {
		if directFailure.set(err) {
			_ = conn.Close()
		}
	}

	var adaptive *adaptiveHLS
	minimumReadySegments := 2
	if len(auth.Stream.Qualities) > 0 {
		if !s.reserveTranscoder() {
			return fmt.Errorf("live transcoder capacity reached")
		}
		defer s.releaseTranscoder()

		adaptive, err = s.startAdaptiveHLS(ctx, reader, auth.Stream.Qualities, hlsDir, publicID, conn, stats)
		if err != nil {
			return err
		}
		session.closeFn = adaptive.close
		minimumReadySegments = 1
	} else {
		hlsTracks, trackMap, err := hlsTracksFromRTMP(reader.Tracks())
		if err != nil {
			return err
		}

		// Legacy streams without an encoding profile retain source-quality remuxing.
		muxer := &gohlslib.Muxer{
			Variant:      gohlslib.MuxerVariantFMP4,
			SegmentCount: 8,
			Tracks:       hlsTracks,
			Directory:    hlsDir,
			OnEncodeError: func(err error) {
				s.log.Warn("hls encode error", "err", err, "public_id", publicID)
			},
		}

		if err := muxer.Start(); err != nil {
			return fmt.Errorf("start hls muxer: %w", err)
		}
		session.muxer = muxer
		wireRTMPToHLS(reader, trackMap, muxer, s.log, publicID, reportDirectFailure, stats)
	}

	if !s.putLiveSession(session) {
		session.close()
		return fmt.Errorf("stream %s registration invariant violated", publicID)
	}
	pump := startRTMPReader(conn, reader)
	diagnosticsCtx, stopDiagnostics := context.WithCancel(ctx)
	defer stopDiagnostics()
	go s.logLiveDiagnostics(diagnosticsCtx, publicID, hlsDir, stats)
	defer func() {
		session.markClosing()
		_ = conn.Close()
		if adaptive != nil {
			adaptive.interruptRelay()
		}
		_ = pump.wait()
		session.close()
		s.removeLiveSession(publicID, session)
	}()

	outputFailure := directFailure.get
	if adaptive != nil {
		outputFailure = adaptive.currentFailure
	}
	readyState, err := waitForHLSReady(startupCtx, hlsDir, minimumReadySegments, pump.done, pump.err, outputFailure)
	if err != nil {
		return err
	}
	if failure := outputFailure(); failure != nil {
		return failure
	}
	if err := pump.completedError(); err != nil {
		return fmt.Errorf("RTMP input ended before readiness registration: %w", err)
	}
	if !session.markReady() {
		return fmt.Errorf("stream %s closed while becoming ready", publicID)
	}
	renditionReadiness := make(map[string]any, len(readyState.Renditions))
	for name, rendition := range readyState.Renditions {
		renditionReadiness[name] = map[string]any{
			"segments":            rendition.SegmentCount,
			"first_segment_after": rendition.First.Sub(startupStartedAt),
		}
	}
	s.log.Info("hls output ready", append(stats.snapshot(), "public_id", publicID, "elapsed", time.Since(startupStartedAt), "renditions", renditionReadiness)...)
	if adaptive != nil {
		adaptive.startWatchdog(ctx, hlsDir, s.cfg.FFmpegStallTimeout)
	}

	var startResp SessionStartedResponse
	if err := s.laravel.Post(ctx, "/internal/live/session-started", SessionStartedRequest{
		PublicID:   publicID,
		ExternalID: randomHex(16),
		HLSPrefix:  path.Join("live", publicID),
	}, &startResp); err != nil {
		return fmt.Errorf("session start failed: %w", err)
	}

	session.sessionID = startResp.SessionID

	if startResp.SessionID == "" {
		return fmt.Errorf("session start returned an empty session id")
	}
	s.log.Info("live session registered", "public_id", publicID, "elapsed", time.Since(startupStartedAt))

	startedAt := time.Now().UTC()
	s.tracker.StartSession(publicID, startResp.SessionID, SessionContext{
		OrganizationID: auth.Stream.OrganizationID,
		LiveStreamID:   auth.Stream.ID,
	})
	if s.analyticsOutbox != nil {
		if err := s.analyticsOutbox.Enqueue(AnalyticsEventBatch{Events: []AnalyticsEvent{{
			EventID:        uuid.NewString(),
			EventType:      AnalyticsSessionStarted,
			SchemaVersion:  1,
			Sequence:       1,
			OccurredAt:     startedAt,
			OrganizationID: auth.Stream.OrganizationID,
			LiveStreamID:   auth.Stream.ID,
			SessionID:      startResp.SessionID,
			Status:         "live",
			StartedAt:      &startedAt,
		}}}); err != nil {
			s.log.Error("analytics session start could not be persisted", "err", err, "public_id", publicID)
		}
	}
	defer func() {
		if adaptive != nil {
			if failure := adaptive.currentFailure(); failure != nil {
				s.log.Warn("adaptive live session failed", "public_id", publicID, "err", failure)
				s.failRTMPSession(publicID, startResp.SessionID, failure)
				return
			}
		}
		if failure := directFailure.get(); failure != nil {
			s.failRTMPSession(publicID, startResp.SessionID, failure)
			return
		}
		s.endRTMPSession(publicID, startResp.SessionID)
	}()

	readErr := pump.wait()
	if failure := outputFailure(); failure != nil {
		return failure
	}

	return readErr
}

type sessionFailure struct {
	once sync.Once
	mu   sync.RWMutex
	err  error
}

func (failure *sessionFailure) set(err error) bool {
	if err == nil {
		return false
	}
	set := false
	failure.once.Do(func() {
		failure.mu.Lock()
		failure.err = err
		failure.mu.Unlock()
		set = true
	})

	return set
}

func (failure *sessionFailure) get() error {
	failure.mu.RLock()
	defer failure.mu.RUnlock()

	return failure.err
}

type rtmpReaderPump struct {
	done    chan struct{}
	mu      sync.RWMutex
	readErr error
}

func startRTMPReader(conn net.Conn, reader *gortmplib.Reader) *rtmpReaderPump {
	pump := &rtmpReaderPump{done: make(chan struct{})}
	go func() {
		defer close(pump.done)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			if err := reader.Read(); err != nil {
				pump.mu.Lock()
				pump.readErr = err
				pump.mu.Unlock()
				return
			}
		}
	}()

	return pump
}

func (pump *rtmpReaderPump) err() error {
	pump.mu.RLock()
	defer pump.mu.RUnlock()

	return pump.readErr
}

func (pump *rtmpReaderPump) completedError() error {
	select {
	case <-pump.done:
		return pump.err()
	default:
		return nil
	}
}

func (pump *rtmpReaderPump) wait() error {
	<-pump.done
	return pump.err()
}

func (s *Server) logLiveDiagnostics(ctx context.Context, publicID string, hlsDir string, stats *relayStats) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state, err := inspectHLSOutput(hlsDir, 1)
			s.log.Info("live media diagnostics", append(stats.snapshot(), "public_id", publicID, "rendition_count", len(state.Renditions), "playlist_error", err)...)
		}
	}
}

func (s *Server) failRTMPSession(publicID, sessionID string, failure error) {
	endedAt := time.Now().UTC()
	snapshot := s.tracker.PrepareEndSessionSnapshot(publicID, endedAt)
	if snapshot.SessionID == "" {
		snapshot.SessionID = sessionID
	}

	if s.analyticsOutbox != nil && snapshot.OrganizationID != "" && snapshot.LiveStreamID != "" && snapshot.SessionID != "" {
		event := analyticsEventFromSnapshot(snapshot, AnalyticsSessionFailed, endedAt, &endedAt)
		event.Status = "failed"
		if err := s.analyticsOutbox.Enqueue(AnalyticsEventBatch{Events: []AnalyticsEvent{event}}); err != nil {
			s.log.Error("analytics session failure could not be persisted", "err", err, "public_id", publicID)
		}
	}
	s.tracker.EndSession(publicID)

	message := failure.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	payload := map[string]any{
		"public_id":     publicID,
		"session_id":    snapshot.SessionID,
		"error_message": message,
	}

	if err := s.outbox.Enqueue("/internal/live/session-failed", payload); err != nil {
		s.outboxHealthy.Store(false)
		s.log.Error("session failure callback could not be persisted", "err", err, "public_id", publicID)

		fallbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if postErr := s.laravel.Post(fallbackCtx, "/internal/live/session-failed", payload, nil); postErr != nil {
			s.log.Error("session failure callback fallback failed", "err", postErr, "public_id", publicID)
		}
	}
}

func (s *Server) cleanupHLSDir(dir string) {
	if dir == "" || filepath.Clean(dir) == filepath.Clean(s.cfg.HLSRoot) {
		return
	}

	if err := os.RemoveAll(dir); err != nil {
		s.log.Warn("hls dir cleanup failed", "err", err, "dir", dir)
	}
}

func (s *Server) endRTMPSession(publicID, sessionID string) {
	endedAt := time.Now().UTC()
	snapshot := s.tracker.PrepareEndSessionSnapshot(publicID, endedAt)

	if snapshot.SessionID == "" {
		snapshot.SessionID = sessionID
	}
	if s.analyticsOutbox != nil && snapshot.OrganizationID != "" && snapshot.LiveStreamID != "" && snapshot.SessionID != "" {
		if err := s.analyticsOutbox.Enqueue(AnalyticsEventBatch{Events: []AnalyticsEvent{
			analyticsEventFromSnapshot(snapshot, AnalyticsSessionEnded, endedAt, &endedAt),
		}}); err != nil {
			s.log.Error("analytics session end could not be persisted", "err", err, "public_id", publicID)
		}
	}
	// The terminal event has been durably written (or a failure has been
	// recorded) before tracker state is released.
	s.tracker.EndSession(publicID)

	payload := map[string]any{
		"public_id":         publicID,
		"session_id":        snapshot.SessionID,
		"minute":            endedAt.UTC().Truncate(time.Minute).Format(time.RFC3339),
		"current_viewers":   snapshot.CurrentViewers,
		"peak_viewers":      snapshot.PeakViewers,
		"unique_viewers":    snapshot.UniqueViewers,
		"playlist_requests": snapshot.PlaylistRequests,
		"segment_requests":  snapshot.SegmentRequests,
	}

	if err := s.outbox.Enqueue("/internal/live/session-ended", payload); err != nil {
		s.outboxHealthy.Store(false)
		s.log.Error("session end callback could not be persisted", "err", err, "public_id", publicID)

		fallbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if postErr := s.laravel.Post(fallbackCtx, "/internal/live/session-ended", payload, nil); postErr != nil {
			s.log.Error("session end callback fallback failed", "err", postErr, "public_id", publicID)
		}
	}
}

func publishCredentials(u *url.URL) (string, string, error) {
	if u == nil {
		return "", "", fmt.Errorf("missing publish URL")
	}

	publicID := strings.Trim(path.Base(u.Path), "/")
	streamKey := u.Query().Get("key")

	if publicID == "" || publicID == "." || streamKey == "" {
		return "", "", fmt.Errorf("publish name must be {public_id}?key={stream_key}")
	}

	if !validPublicID(publicID) {
		return "", "", fmt.Errorf("invalid public id %q", publicID)
	}

	return publicID, streamKey, nil
}

func hlsTracksFromRTMP(rtmpTracks []*gortmplib.Track) ([]*gohlslib.Track, map[*gortmplib.Track]*gohlslib.Track, error) {
	hlsTracks := make([]*gohlslib.Track, 0, len(rtmpTracks))
	trackMap := make(map[*gortmplib.Track]*gohlslib.Track, len(rtmpTracks))

	for _, rtmpTrack := range rtmpTracks {
		hlsTrack, err := hlsTrackFromRTMP(rtmpTrack)
		if err != nil {
			return nil, nil, err
		}

		hlsTracks = append(hlsTracks, hlsTrack)
		trackMap[rtmpTrack] = hlsTrack
	}

	return hlsTracks, trackMap, nil
}

func hlsTrackFromRTMP(track *gortmplib.Track) (*gohlslib.Track, error) {
	switch codec := track.Codec.(type) {
	case *rtmpcodecs.H264:
		return &gohlslib.Track{
			Codec: &hlscodecs.H264{
				SPS: codec.SPS,
				PPS: codec.PPS,
			},
			ClockRate: 90000,
		}, nil

	case *rtmpcodecs.H265:
		return &gohlslib.Track{
			Codec: &hlscodecs.H265{
				VPS: codec.VPS,
				SPS: codec.SPS,
				PPS: codec.PPS,
			},
			ClockRate: 90000,
		}, nil

	case *rtmpcodecs.MPEG4Audio:
		if codec.Config == nil {
			return nil, fmt.Errorf("aac track is missing config")
		}

		return &gohlslib.Track{
			Codec: &hlscodecs.MPEG4Audio{
				Config: *codec.Config,
			},
			ClockRate: codec.Config.SampleRate,
		}, nil

	case *rtmpcodecs.Opus:
		return &gohlslib.Track{
			Codec: &hlscodecs.Opus{
				ChannelCount: codec.ChannelCount,
			},
			ClockRate: 48000,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported rtmp codec %T", codec)
	}
}

func wireRTMPToHLS(
	reader *gortmplib.Reader,
	trackMap map[*gortmplib.Track]*gohlslib.Track,
	muxer *gohlslib.Muxer,
	log logger,
	publicID string,
	reportFailure func(error),
	stats *relayStats,
) {
	for _, rtmpTrack := range reader.Tracks() {
		hlsTrack := trackMap[rtmpTrack]

		switch codec := rtmpTrack.Codec.(type) {
		case *rtmpcodecs.H264:
			filter := h264RelayFilter{sps: codec.SPS, pps: codec.PPS}
			reader.OnDataH264(rtmpTrack, func(pts time.Duration, dts time.Duration, au [][]byte) {
				au = filter.process(au)
				if au == nil {
					return
				}
				stats.recordVideo(dts, h264HasIDR(au))
				if err := muxer.WriteH264(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), au); err != nil {
					log.Warn("write h264 failed", "err", err, "public_id", publicID)
					reportFailure(fmt.Errorf("write HLS h264: %w", err))
				}
			})

		case *rtmpcodecs.H265:
			vps, sps, pps := codec.VPS, codec.SPS, codec.PPS
			reader.OnDataH265(rtmpTrack, func(pts time.Duration, dts time.Duration, au [][]byte) {
				stats.recordVideo(dts, h265HasRandomAccess(au))
				au = withH265ParameterSets(au, vps, sps, pps)
				if err := muxer.WriteH265(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), au); err != nil {
					log.Warn("write h265 failed", "err", err, "public_id", publicID)
					reportFailure(fmt.Errorf("write HLS h265: %w", err))
				}
			})

		case *rtmpcodecs.MPEG4Audio:
			reader.OnDataMPEG4Audio(rtmpTrack, func(pts time.Duration, au []byte) {
				stats.recordAudio(pts)
				if err := muxer.WriteMPEG4Audio(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), [][]byte{au}); err != nil {
					log.Warn("write aac failed", "err", err, "public_id", publicID)
					reportFailure(fmt.Errorf("write HLS aac: %w", err))
				}
			})

		case *rtmpcodecs.Opus:
			reader.OnDataOpus(rtmpTrack, func(pts time.Duration, packet []byte) {
				stats.recordAudio(pts)
				if err := muxer.WriteOpus(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), [][]byte{packet}); err != nil {
					log.Warn("write opus failed", "err", err, "public_id", publicID)
					reportFailure(fmt.Errorf("write HLS opus: %w", err))
				}
			})
		}
	}
}

// H.264/H.265 NALU types needed to detect key frames and parameter sets.
const (
	h264NALUTypeIDR = 5
	h264NALUTypeSPS = 7
	h264NALUTypePPS = 8

	h265NALUTypeVPS = 32
	h265NALUTypeSPS = 33
	h265NALUTypePPS = 34

	// IRAP (random access) NALU types span BLA_W_LP..RSV_IRAP_VCL23.
	h265NALUTypeIRAPFirst = 16
	h265NALUTypeIRAPLast  = 23
)

func h264HasIDR(au [][]byte) bool {
	for _, nalu := range au {
		if len(nalu) != 0 && nalu[0]&0x1f == h264NALUTypeIDR {
			return true
		}
	}

	return false
}

func h265HasRandomAccess(au [][]byte) bool {
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		naluType := (nalu[0] >> 1) & 0x3f
		if naluType >= h265NALUTypeIRAPFirst && naluType <= h265NALUTypeIRAPLast {
			return true
		}
	}

	return false
}

// withH264ParameterSets prepends the SPS/PPS carried by the RTMP AVC sequence
// header to every key frame that does not already repeat them in-band.
//
// Many publishers (OBS among them) send parameter sets only once, in the
// sequence header. gohlslib builds its DTS extractor from in-band NALUs only,
// so without this the muxer fails every write with "SPS not received yet" and
// never produces a segment or a playlist.
func withH264ParameterSets(au [][]byte, sps, pps []byte) [][]byte {
	if len(sps) == 0 || len(pps) == 0 {
		return au
	}

	keyFrame := false
	hasSPS := false
	hasPPS := false

	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}

		switch nalu[0] & 0x1F {
		case h264NALUTypeIDR:
			keyFrame = true
		case h264NALUTypeSPS:
			hasSPS = true
		case h264NALUTypePPS:
			hasPPS = true
		}
	}

	if !keyFrame || (hasSPS && hasPPS) {
		return au
	}

	return prependNALUs(au, sps, pps)
}

// withH265ParameterSets is the HEVC counterpart of withH264ParameterSets.
func withH265ParameterSets(au [][]byte, vps, sps, pps []byte) [][]byte {
	if len(vps) == 0 || len(sps) == 0 || len(pps) == 0 {
		return au
	}

	keyFrame := false
	hasVPS := false
	hasSPS := false
	hasPPS := false

	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}

		typ := (nalu[0] >> 1) & 0x3F

		switch {
		case typ >= h265NALUTypeIRAPFirst && typ <= h265NALUTypeIRAPLast:
			keyFrame = true
		case typ == h265NALUTypeVPS:
			hasVPS = true
		case typ == h265NALUTypeSPS:
			hasSPS = true
		case typ == h265NALUTypePPS:
			hasPPS = true
		}
	}

	if !keyFrame || (hasVPS && hasSPS && hasPPS) {
		return au
	}

	return prependNALUs(au, vps, sps, pps)
}

// prependNALUs returns a new access unit with the given NALUs placed in front,
// leaving the caller's slice untouched.
func prependNALUs(au [][]byte, nalus ...[]byte) [][]byte {
	out := make([][]byte, 0, len(nalus)+len(au))
	out = append(out, nalus...)

	return append(out, au...)
}

type logger interface {
	Warn(msg string, args ...any)
}

func toClock(pts time.Duration, clockRate int) int64 {
	return int64(pts/time.Second)*int64(clockRate) + int64(pts%time.Second)*int64(clockRate)/int64(time.Second)
}
