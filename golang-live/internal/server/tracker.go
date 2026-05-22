package server

import (
	"sync"
	"time"
)

type Tracker struct {
	mu      sync.Mutex
	streams map[string]*streamMetrics
	ttl     time.Duration
}

type streamMetrics struct {
	SessionID        string
	Viewers          map[string]time.Time
	UniqueViewers    map[string]struct{}
	PlaylistRequests int64
	SegmentRequests  int64
	PeakViewers      int
}

type Snapshot struct {
	PublicID         string
	SessionID        string
	CurrentViewers   int
	UniqueViewers    int
	PlaylistRequests int64
	SegmentRequests  int64
	PeakViewers      int
}

func NewTracker(ttl time.Duration) *Tracker {
	return &Tracker{
		streams: make(map[string]*streamMetrics),
		ttl:     ttl,
	}
}

func (t *Tracker) StartSession(publicID, sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	m := t.metrics(publicID)
	m.SessionID = sessionID
	m.Viewers = make(map[string]time.Time)
	m.UniqueViewers = make(map[string]struct{})
	m.PlaylistRequests = 0
	m.SegmentRequests = 0
	m.PeakViewers = 0
}

func (t *Tracker) EndSession(publicID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.streams, publicID)
}

func (t *Tracker) EndSessionSnapshot(publicID string, now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	m, ok := t.streams[publicID]
	if !ok {
		return Snapshot{PublicID: publicID}
	}

	snapshot := Snapshot{
		PublicID:         publicID,
		SessionID:        m.SessionID,
		CurrentViewers:   t.currentLocked(m, now),
		UniqueViewers:    len(m.UniqueViewers),
		PlaylistRequests: m.PlaylistRequests,
		SegmentRequests:  m.SegmentRequests,
		PeakViewers:      m.PeakViewers,
	}

	delete(t.streams, publicID)

	return snapshot
}

func (t *Tracker) Observe(publicID, viewerID, path string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	m := t.metrics(publicID)
	m.Viewers[viewerID] = now
	m.UniqueViewers[viewerID] = struct{}{}

	if isPlaylist(path) {
		m.PlaylistRequests++
	} else {
		m.SegmentRequests++
	}

	current := t.currentLocked(m, now)
	if current > m.PeakViewers {
		m.PeakViewers = current
	}
}

func (t *Tracker) Snapshots(now time.Time) []Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	snapshots := make([]Snapshot, 0, len(t.streams))
	for publicID, m := range t.streams {
		current := t.currentLocked(m, now)
		snapshots = append(snapshots, Snapshot{
			PublicID:         publicID,
			SessionID:        m.SessionID,
			CurrentViewers:   current,
			UniqueViewers:    len(m.UniqueViewers),
			PlaylistRequests: m.PlaylistRequests,
			SegmentRequests:  m.SegmentRequests,
			PeakViewers:      m.PeakViewers,
		})
	}

	return snapshots
}

func (t *Tracker) metrics(publicID string) *streamMetrics {
	m, ok := t.streams[publicID]
	if ok {
		return m
	}

	m = &streamMetrics{
		Viewers:       make(map[string]time.Time),
		UniqueViewers: make(map[string]struct{}),
	}
	t.streams[publicID] = m

	return m
}

func (t *Tracker) currentLocked(m *streamMetrics, now time.Time) int {
	cutoff := now.Add(-t.ttl)
	for viewerID, seenAt := range m.Viewers {
		if seenAt.Before(cutoff) {
			delete(m.Viewers, viewerID)
		}
	}

	return len(m.Viewers)
}

func isPlaylist(path string) bool {
	return len(path) >= 5 && path[len(path)-5:] == ".m3u8"
}
