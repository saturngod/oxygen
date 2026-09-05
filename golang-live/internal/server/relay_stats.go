package server

import (
	"sync"
	"time"
)

// Counts input callbacks, including video sequence headers, without retaining media.
type relayStats struct {
	forwarded          uint64
	mu                 sync.Mutex
	video, audio       uint64
	videoDTS, audioPTS time.Duration
	videoAt, audioAt   time.Time
}

func (s *relayStats) record(kind string, timestamp time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == "video" {
		s.video++
		s.videoDTS = timestamp
		s.videoAt = time.Now()
	} else {
		s.audio++
		s.audioPTS = timestamp
		s.audioAt = time.Now()
	}
}

func (s *relayStats) snapshot() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []any{"video_callbacks", s.video, "audio_callbacks", s.audio,
		"video_dts", s.videoDTS, "audio_pts", s.audioPTS,
		"last_video_at", s.videoAt, "last_audio_at", s.audioAt, "relay_writes", s.forwarded}
}
