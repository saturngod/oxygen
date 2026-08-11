package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventSessionStarted EventType = "session.started.v1"
	EventViewerSample   EventType = "viewer.sample.v1"
	EventSessionEnded   EventType = "session.ended.v1"
	EventSessionFailed  EventType = "session.failed.v1"
)

func (e EventType) Valid() bool {
	switch e {
	case EventSessionStarted, EventViewerSample, EventSessionEnded, EventSessionFailed:
		return true
	default:
		return false
	}
}

type Event struct {
	EventID           uuid.UUID  `json:"event_id"`
	EventType         EventType  `json:"event_type"`
	SchemaVersion     int        `json:"schema_version"`
	Sequence          int64      `json:"sequence"`
	OccurredAt        time.Time  `json:"occurred_at"`
	OrganizationID    uuid.UUID  `json:"organization_id"`
	LiveStreamID      uuid.UUID  `json:"live_stream_id"`
	SessionID         uuid.UUID  `json:"live_stream_session_id"`
	Status            string     `json:"status,omitempty"`
	CurrentViewers    int        `json:"current_viewers"`
	IntervalPeak      int        `json:"interval_peak_viewers"`
	SessionPeak       int        `json:"session_peak_viewers"`
	IdentityAdditions int64      `json:"viewer_identity_additions"`
	PlaylistDelta     int64      `json:"playlist_requests_delta"`
	SegmentDelta      int64      `json:"segment_requests_delta"`
	UniqueTotal       int64      `json:"unique_viewers_total"`
	PlaylistTotal     int64      `json:"playlist_requests_total"`
	SegmentTotal      int64      `json:"segment_requests_total"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
}

type EventBatch struct {
	Events []Event `json:"events"`
}

type IngestResult struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
}

func (e Event) Validate(maxAge time.Duration, now time.Time) error {
	if e.EventID == uuid.Nil || e.OrganizationID == uuid.Nil || e.LiveStreamID == uuid.Nil || e.SessionID == uuid.Nil {
		return fmt.Errorf("event and entity IDs are required")
	}
	if !e.EventType.Valid() {
		return fmt.Errorf("unknown event type %q", e.EventType)
	}
	if e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema version %d", e.SchemaVersion)
	}
	if e.Sequence < 1 {
		return fmt.Errorf("sequence must be positive")
	}
	if e.OccurredAt.IsZero() || e.OccurredAt.Location() == nil {
		return fmt.Errorf("occurred_at must include a timestamp")
	}
	if e.OccurredAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("occurred_at is too far in the future")
	}
	if maxAge > 0 && e.OccurredAt.Before(now.Add(-maxAge)) && e.EventType == EventViewerSample {
		return fmt.Errorf("viewer event is older than retention window")
	}
	if e.CurrentViewers < 0 || e.IntervalPeak < 0 || e.SessionPeak < 0 || e.IdentityAdditions < 0 || e.PlaylistDelta < 0 || e.SegmentDelta < 0 || e.UniqueTotal < 0 || e.PlaylistTotal < 0 || e.SegmentTotal < 0 {
		return fmt.Errorf("event counters cannot be negative")
	}

	return nil
}

type HourlyMetric struct {
	BucketStart             time.Time
	PeakViewers             int
	ViewerIdentityAdditions int64
	PlaylistRequests        int64
	SegmentRequests         int64
	SampleCount             int64
}

type SessionMetric struct {
	SessionID        uuid.UUID
	OrganizationID   uuid.UUID
	LiveStreamID     uuid.UUID
	Status           string
	StartedAt        *time.Time
	EndedAt          *time.Time
	CurrentViewers   int
	PeakViewers      int
	UniqueViewers    int64
	PlaylistRequests int64
	SegmentRequests  int64
	LastSequence     int64
	LastEventAt      *time.Time
}
