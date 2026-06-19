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
	"time"

	"github.com/bluenviron/gohlslib/v2"
	hlscodecs "github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/gortmplib"
	rtmpcodecs "github.com/bluenviron/gortmplib/pkg/codecs"
)

func (s *Server) RunRTMP(ctx context.Context) {
	if s.cfg.RTMPAddr == "" {
		return
	}

	ln, err := net.Listen("tcp", s.cfg.RTMPAddr)
	if err != nil {
		s.log.Error("rtmp listener failed", "err", err, "addr", s.cfg.RTMPAddr)
		return
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	s.log.Info("rtmp ingest listening", "addr", s.cfg.RTMPAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}

			s.log.Warn("rtmp accept failed", "err", err)
			continue
		}

		go s.handleRTMPConn(conn)
	}
}

func (s *Server) handleRTMPConn(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	s.log.Info("rtmp connection opened", "remote_addr", remoteAddr)

	if err := s.handleRTMPConnInner(conn); err != nil {
		s.log.Warn("rtmp connection closed", "remote_addr", remoteAddr, "err", err)
		return
	}

	s.log.Info("rtmp connection closed", "remote_addr", remoteAddr)
}

func (s *Server) handleRTMPConnInner(conn net.Conn) error {
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
	if err := s.laravel.Post(context.Background(), "/internal/live/auth-publish", AuthPublishRequest{
		PublicID:  publicID,
		StreamKey: streamKey,
	}, &auth); err != nil {
		return fmt.Errorf("publish auth failed: %w", err)
	}

	if !auth.Allowed {
		return fmt.Errorf("publish auth rejected")
	}

	if s.getLiveSession(publicID) != nil {
		return fmt.Errorf("stream %s is already active", publicID)
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := &gortmplib.Reader{Conn: rtmpConn}
	if err := reader.Initialize(); err != nil {
		return fmt.Errorf("initialize rtmp reader: %w", err)
	}

	hlsTracks, trackMap, err := hlsTracksFromRTMP(reader.Tracks())
	if err != nil {
		return err
	}

	hlsDir := filepath.Join(s.cfg.HLSRoot, publicID)
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		return fmt.Errorf("create hls dir: %w", err)
	}

	muxer := &gohlslib.Muxer{
		Tracks:    hlsTracks,
		Directory: hlsDir,
		OnEncodeError: func(err error) {
			s.log.Warn("hls encode error", "err", err, "public_id", publicID)
		},
	}

	if err := muxer.Start(); err != nil {
		return fmt.Errorf("start hls muxer: %w", err)
	}

	session := &liveSession{
		publicID: publicID,
		muxer:    muxer,
		conn:     conn,
	}

	// Registered before the session-started callback so the muxer is always
	// closed and the on-disk HLS tree is always reaped, even if that callback
	// fails. defer LIFO order on return: end callback -> unregister -> close
	// muxer -> remove directory.
	defer s.cleanupHLSDir(hlsDir)
	defer session.close()

	var startResp SessionStartedResponse
	if err := s.laravel.Post(context.Background(), "/internal/live/session-started", SessionStartedRequest{
		PublicID:   publicID,
		ExternalID: randomHex(16),
		HLSPrefix:  path.Join("live", publicID),
	}, &startResp); err != nil {
		return fmt.Errorf("session start failed: %w", err)
	}

	session.sessionID = startResp.SessionID

	if !s.putLiveSession(session) {
		_ = s.laravel.Post(context.Background(), "/internal/live/session-failed", map[string]any{
			"public_id":     publicID,
			"session_id":    startResp.SessionID,
			"error_message": "stream is already active",
		}, nil)

		return fmt.Errorf("stream %s is already active", publicID)
	}
	defer s.removeLiveSession(publicID, session)

	s.tracker.StartSession(publicID, startResp.SessionID)
	defer s.endRTMPSession(publicID, startResp.SessionID)

	wireRTMPToHLS(reader, trackMap, muxer, s.log, publicID)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if err := reader.Read(); err != nil {
			return err
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
	snapshot := s.tracker.EndSessionSnapshot(publicID, time.Now())

	if snapshot.SessionID == "" {
		snapshot.SessionID = sessionID
	}

	err := s.laravel.Post(context.Background(), "/internal/live/session-ended", map[string]any{
		"public_id":         publicID,
		"session_id":        snapshot.SessionID,
		"peak_viewers":      snapshot.PeakViewers,
		"unique_viewers":    snapshot.UniqueViewers,
		"playlist_requests": snapshot.PlaylistRequests,
		"segment_requests":  snapshot.SegmentRequests,
	}, nil)
	if err != nil {
		s.log.Warn("session end callback failed", "err", err, "public_id", publicID)
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
) {
	for _, rtmpTrack := range reader.Tracks() {
		hlsTrack := trackMap[rtmpTrack]

		switch rtmpTrack.Codec.(type) {
		case *rtmpcodecs.H264:
			reader.OnDataH264(rtmpTrack, func(pts time.Duration, _ time.Duration, au [][]byte) {
				if err := muxer.WriteH264(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), au); err != nil {
					log.Warn("write h264 failed", "err", err, "public_id", publicID)
				}
			})

		case *rtmpcodecs.H265:
			reader.OnDataH265(rtmpTrack, func(pts time.Duration, _ time.Duration, au [][]byte) {
				if err := muxer.WriteH265(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), au); err != nil {
					log.Warn("write h265 failed", "err", err, "public_id", publicID)
				}
			})

		case *rtmpcodecs.MPEG4Audio:
			reader.OnDataMPEG4Audio(rtmpTrack, func(pts time.Duration, au []byte) {
				if err := muxer.WriteMPEG4Audio(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), [][]byte{au}); err != nil {
					log.Warn("write aac failed", "err", err, "public_id", publicID)
				}
			})

		case *rtmpcodecs.Opus:
			reader.OnDataOpus(rtmpTrack, func(pts time.Duration, packet []byte) {
				if err := muxer.WriteOpus(hlsTrack, time.Now(), toClock(pts, hlsTrack.ClockRate), [][]byte{packet}); err != nil {
					log.Warn("write opus failed", "err", err, "public_id", publicID)
				}
			})
		}
	}
}

type logger interface {
	Warn(msg string, args ...any)
}

func toClock(pts time.Duration, clockRate int) int64 {
	return int64(pts) * int64(clockRate) / int64(time.Second)
}
