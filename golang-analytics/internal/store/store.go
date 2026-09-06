package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"oxygen/analytics/internal/domain"
)

type EventStore interface {
	IngestBatch(context.Context, []domain.Event) (domain.IngestResult, error)
}

type AnalyticsStore interface {
	QueryHourly(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]domain.HourlyMetric, error)
	CountOverlappingSessions(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (int64, error)
	CurrentSession(context.Context, uuid.UUID, uuid.UUID) (*domain.SessionMetric, error)
	CoverageStart(context.Context) (*time.Time, error)
	LatestEventAt(context.Context, uuid.UUID, uuid.UUID) (*time.Time, error)
}

type PurgeStore interface {
	PurgeStream(context.Context, uuid.UUID, uuid.UUID) error
}

type MaintenanceStore interface {
	Reconcile(context.Context, time.Time, time.Time) error
	PruneEvents(context.Context, time.Time) (int64, error)
}

type Store interface {
	EventStore
	AnalyticsStore
	PurgeStore
	MaintenanceStore
	Ping(context.Context) error
	Close()
}
