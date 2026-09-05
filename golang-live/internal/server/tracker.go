package server

import (
	"container/list"
	"sync"
	"time"
)

type Tracker struct {
	mu                sync.Mutex
	streams           map[string]*streamMetrics
	ttl               time.Duration
	maxTrackedViewers int
}

type SessionContext struct {
	OrganizationID string
	LiveStreamID   string
}

type streamMetrics struct {
	SessionID              string
	OrganizationID         string
	LiveStreamID           string
	Viewers                map[string]time.Time
	viewerOrder            *list.List
	viewerElements         map[string]*list.Element
	UniqueViewers          map[string]struct{}
	PlaylistRequests       int64
	SegmentRequests        int64
	IdentityAdditionsDelta int64
	PlaylistRequestsDelta  int64
	SegmentRequestsDelta   int64
	IntervalPeakViewers    int
	PeakViewers            int
	AnalyticsSequence      int64
	AnalyticsGeneration    uint64
}

type Snapshot struct {
	PublicID              string
	SessionID             string
	OrganizationID        string
	LiveStreamID          string
	CurrentViewers        int
	UniqueViewers         int
	PlaylistRequests      int64
	SegmentRequests       int64
	IdentityAdditions     int64
	PlaylistRequestsDelta int64
	SegmentRequestsDelta  int64
	IntervalPeakViewers   int
	PeakViewers           int
	AnalyticsSequence     int64
	AnalyticsGeneration   uint64
}

func NewTracker(ttl time.Duration, maxTrackedViewers int) *Tracker {
	return &Tracker{
		streams:           make(map[string]*streamMetrics),
		ttl:               ttl,
		maxTrackedViewers: maxTrackedViewers,
	}
}

func (t *Tracker) StartSession(publicID, sessionID string, contexts ...SessionContext) {
	t.mu.Lock()
	defer t.mu.Unlock()

	m := t.metrics(publicID)
	m.SessionID = sessionID
	m.Viewers = make(map[string]time.Time)
	m.viewerOrder = list.New()
	m.viewerElements = make(map[string]*list.Element)
	m.UniqueViewers = make(map[string]struct{})
	m.PlaylistRequests = 0
	m.SegmentRequests = 0
	m.IdentityAdditionsDelta = 0
	m.PlaylistRequestsDelta = 0
	m.SegmentRequestsDelta = 0
	m.IntervalPeakViewers = 0
	m.PeakViewers = 0
	m.AnalyticsSequence = 0
	m.AnalyticsGeneration = 0
	if len(contexts) > 0 {
		m.OrganizationID = contexts[0].OrganizationID
		m.LiveStreamID = contexts[0].LiveStreamID
		// Sequence one is reserved for session.started.v1.
		m.AnalyticsSequence = 1
	}
}

func (t *Tracker) EndSession(publicID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.streams, publicID)
}

func (t *Tracker) EndSessionSnapshot(publicID string, now time.Time) Snapshot {
	snapshot := t.PrepareEndSessionSnapshot(publicID, now)
	t.EndSession(publicID)
	return snapshot
}

func (t *Tracker) PrepareEndSessionSnapshot(publicID string, now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	m, ok := t.streams[publicID]
	if !ok {
		return Snapshot{PublicID: publicID}
	}

	snapshot := Snapshot{
		PublicID:              publicID,
		SessionID:             m.SessionID,
		OrganizationID:        m.OrganizationID,
		LiveStreamID:          m.LiveStreamID,
		CurrentViewers:        t.currentLocked(m, now),
		UniqueViewers:         len(m.UniqueViewers),
		PlaylistRequests:      m.PlaylistRequests,
		SegmentRequests:       m.SegmentRequests,
		IdentityAdditions:     m.IdentityAdditionsDelta,
		PlaylistRequestsDelta: m.PlaylistRequestsDelta,
		SegmentRequestsDelta:  m.SegmentRequestsDelta,
		IntervalPeakViewers:   m.IntervalPeakViewers,
		PeakViewers:           m.PeakViewers,
		AnalyticsSequence:     m.AnalyticsSequence + 1,
	}

	return snapshot
}

func (t *Tracker) Observe(publicID, viewerID, path string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	m, ok := t.streams[publicID]
	if !ok || m.SessionID == "" {
		return false
	}

	t.currentLocked(m, now)
	if _, exists := m.Viewers[viewerID]; exists || len(m.Viewers) < t.maxTrackedViewers {
		m.Viewers[viewerID] = now
		if element := m.viewerElements[viewerID]; element != nil {
			m.viewerOrder.MoveToBack(element)
		} else {
			m.viewerElements[viewerID] = m.viewerOrder.PushBack(viewerID)
		}
	}
	if _, exists := m.UniqueViewers[viewerID]; !exists && len(m.UniqueViewers) < t.maxTrackedViewers {
		m.UniqueViewers[viewerID] = struct{}{}
		m.IdentityAdditionsDelta++
	}

	if isPlaylist(path) {
		m.PlaylistRequests++
		m.PlaylistRequestsDelta++
	} else {
		m.SegmentRequests++
		m.SegmentRequestsDelta++
	}

	current := len(m.Viewers)
	if current > m.PeakViewers {
		m.PeakViewers = current
	}
	if current > m.IntervalPeakViewers {
		m.IntervalPeakViewers = current
	}
	m.AnalyticsGeneration++

	return true
}

func (t *Tracker) Snapshots(now time.Time) []Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	snapshots := make([]Snapshot, 0, len(t.streams))
	for publicID, m := range t.streams {
		current := t.currentLocked(m, now)
		snapshots = append(snapshots, Snapshot{
			PublicID:              publicID,
			SessionID:             m.SessionID,
			OrganizationID:        m.OrganizationID,
			LiveStreamID:          m.LiveStreamID,
			CurrentViewers:        current,
			UniqueViewers:         len(m.UniqueViewers),
			PlaylistRequests:      m.PlaylistRequests,
			SegmentRequests:       m.SegmentRequests,
			IdentityAdditions:     m.IdentityAdditionsDelta,
			PlaylistRequestsDelta: m.PlaylistRequestsDelta,
			SegmentRequestsDelta:  m.SegmentRequestsDelta,
			IntervalPeakViewers:   m.IntervalPeakViewers,
			PeakViewers:           m.PeakViewers,
			AnalyticsSequence:     m.AnalyticsSequence + 1,
			AnalyticsGeneration:   m.AnalyticsGeneration,
		})
	}

	return snapshots
}

// PrepareAnalyticsBatch snapshots interval deltas without clearing them. The
// caller must acknowledge the returned snapshots only after their events have
// been atomically persisted to the local analytics outbox.
func (t *Tracker) PrepareAnalyticsBatch(now time.Time) []Snapshot {
	return t.Snapshots(now)
}

func (t *Tracker) AcknowledgeAnalyticsBatch(snapshots []Snapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, snapshot := range snapshots {
		m, ok := t.streams[snapshot.PublicID]
		if !ok || m.SessionID != snapshot.SessionID {
			continue
		}
		if snapshot.AnalyticsSequence > m.AnalyticsSequence {
			m.AnalyticsSequence = snapshot.AnalyticsSequence
		}
		m.IdentityAdditionsDelta -= minInt64(m.IdentityAdditionsDelta, snapshot.IdentityAdditions)
		m.PlaylistRequestsDelta -= minInt64(m.PlaylistRequestsDelta, snapshot.PlaylistRequestsDelta)
		m.SegmentRequestsDelta -= minInt64(m.SegmentRequestsDelta, snapshot.SegmentRequestsDelta)
		if m.AnalyticsGeneration == snapshot.AnalyticsGeneration {
			m.IntervalPeakViewers = 0
		}
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
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
	for m.viewerOrder != nil && m.viewerOrder.Len() > 0 {
		element := m.viewerOrder.Front()
		viewerID := element.Value.(string)
		if !m.Viewers[viewerID].Before(cutoff) {
			break
		}
		delete(m.Viewers, viewerID)
		delete(m.viewerElements, viewerID)
		m.viewerOrder.Remove(element)
	}

	return len(m.Viewers)
}

func isPlaylist(path string) bool {
	return len(path) >= 5 && path[len(path)-5:] == ".m3u8"
}
