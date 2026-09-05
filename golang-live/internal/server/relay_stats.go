package server

import (
	"sync"
	"time"
)

// Counts decoded media callbacks without retaining media.
type relayStats struct {
	forwarded            uint64
	mu                   sync.Mutex
	video, audio         uint64
	videoDTS, audioPTS   time.Duration
	videoAt, audioAt     time.Time
	firstVideoAt         time.Time
	firstAudioAt         time.Time
	firstIDRAt           time.Time
	lastIDRDTS           time.Duration
	lastIDRInterval      time.Duration
	idrCount             uint64
	timestampRegressions uint64
	hasVideoTimestamp    bool
	hasAudioTimestamp    bool
	startedAt            time.Time
}

func newRelayStats(startedAt time.Time) *relayStats {
	return &relayStats{startedAt: startedAt}
}

func (s *relayStats) recordVideo(timestamp time.Duration, randomAccess bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.hasVideoTimestamp && timestamp < s.videoDTS {
		s.timestampRegressions++
	}
	s.video++
	s.videoDTS = timestamp
	s.videoAt = now
	s.hasVideoTimestamp = true
	if s.firstVideoAt.IsZero() {
		s.firstVideoAt = now
	}
	if randomAccess {
		if s.idrCount > 0 {
			s.lastIDRInterval = timestamp - s.lastIDRDTS
		}
		s.idrCount++
		s.lastIDRDTS = timestamp
		if s.firstIDRAt.IsZero() {
			s.firstIDRAt = now
		}
	}
}

func (s *relayStats) recordAudio(timestamp time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.hasAudioTimestamp && timestamp < s.audioPTS {
		s.timestampRegressions++
	}
	s.audio++
	s.audioPTS = timestamp
	s.audioAt = now
	s.hasAudioTimestamp = true
	if s.firstAudioAt.IsZero() {
		s.firstAudioAt = now
	}
}

func (s *relayStats) snapshot() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstVideoAfter time.Duration
	var firstAudioAfter time.Duration
	var firstIDRAfter time.Duration
	if !s.startedAt.IsZero() {
		if !s.firstVideoAt.IsZero() {
			firstVideoAfter = s.firstVideoAt.Sub(s.startedAt)
		}
		if !s.firstAudioAt.IsZero() {
			firstAudioAfter = s.firstAudioAt.Sub(s.startedAt)
		}
		if !s.firstIDRAt.IsZero() {
			firstIDRAfter = s.firstIDRAt.Sub(s.startedAt)
		}
	}
	return []any{"video_callbacks", s.video, "audio_callbacks", s.audio,
		"video_dts", s.videoDTS, "audio_pts", s.audioPTS,
		"av_timestamp_difference", s.videoDTS - s.audioPTS,
		"first_video_after", firstVideoAfter, "first_audio_after", firstAudioAfter,
		"first_idr_after", firstIDRAfter, "last_idr_dts", s.lastIDRDTS,
		"last_idr_interval", s.lastIDRInterval,
		"idr_count", s.idrCount, "timestamp_regressions", s.timestampRegressions,
		"last_video_at", s.videoAt, "last_audio_at", s.audioAt, "relay_writes", s.forwarded}
}
