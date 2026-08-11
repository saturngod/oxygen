package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"oxygen/analytics/internal/domain"
)

func (s *Store) QueryHourly(ctx context.Context, organizationID, streamID uuid.UUID, start, end time.Time) ([]domain.HourlyMetric, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bucket_start, peak_viewers, viewer_identity_additions,
		       playlist_requests, segment_requests, sample_count
		FROM stream_hourly_metrics
		WHERE organization_id = $1
		  AND live_stream_id = $2
		  AND bucket_start >= $3
		  AND bucket_start <= $4
		ORDER BY bucket_start
	`, organizationID, streamID, start.UTC(), end.UTC())
	if err != nil {
		return nil, fmt.Errorf("query hourly analytics: %w", err)
	}
	defer rows.Close()

	metrics := make([]domain.HourlyMetric, 0)
	for rows.Next() {
		var metric domain.HourlyMetric
		if err := rows.Scan(&metric.BucketStart, &metric.PeakViewers, &metric.ViewerIdentityAdditions, &metric.PlaylistRequests, &metric.SegmentRequests, &metric.SampleCount); err != nil {
			return nil, fmt.Errorf("scan hourly analytics: %w", err)
		}
		metric.BucketStart = metric.BucketStart.UTC()
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hourly analytics: %w", err)
	}

	return metrics, nil
}

func (s *Store) CountOverlappingSessions(ctx context.Context, organizationID, streamID uuid.UUID, start, end time.Time) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM session_metrics
		WHERE organization_id = $1
		  AND live_stream_id = $2
		  AND started_at IS NOT NULL
		  AND started_at <= $3
		  AND (ended_at IS NULL OR ended_at >= $4)
	`, organizationID, streamID, end.UTC(), start.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count overlapping analytics sessions: %w", err)
	}
	return count, nil
}

func (s *Store) CurrentSession(ctx context.Context, organizationID, streamID uuid.UUID) (*domain.SessionMetric, error) {
	var metric domain.SessionMetric
	err := s.pool.QueryRow(ctx, `
		SELECT live_stream_session_id, organization_id, live_stream_id, status,
		       started_at, ended_at, current_viewers, peak_viewers, unique_viewers,
		       playlist_requests, segment_requests, last_sequence, last_event_at
		FROM session_metrics
		WHERE organization_id = $1
		  AND live_stream_id = $2
		  AND status IN ('starting', 'live')
		ORDER BY started_at DESC NULLS LAST
		LIMIT 1
	`, organizationID, streamID).Scan(
		&metric.SessionID, &metric.OrganizationID, &metric.LiveStreamID, &metric.Status,
		&metric.StartedAt, &metric.EndedAt, &metric.CurrentViewers, &metric.PeakViewers,
		&metric.UniqueViewers, &metric.PlaylistRequests, &metric.SegmentRequests,
		&metric.LastSequence, &metric.LastEventAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query current analytics session: %w", err)
	}
	return &metric, nil
}

func (s *Store) CoverageStart(ctx context.Context) (*time.Time, error) {
	var value *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT coverage_started_at FROM analytics_service_state WHERE id = 1`).Scan(&value); err != nil {
		return nil, fmt.Errorf("query analytics coverage: %w", err)
	}
	if value != nil {
		utc := value.UTC()
		value = &utc
	}
	return value, nil
}

func (s *Store) LatestEventAt(ctx context.Context, organizationID, streamID uuid.UUID) (*time.Time, error) {
	var value *time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT max(occurred_at)
		FROM analytics_events
		WHERE organization_id = $1 AND live_stream_id = $2
	`, organizationID, streamID).Scan(&value); err != nil {
		return nil, fmt.Errorf("query latest analytics event: %w", err)
	}
	if value != nil {
		utc := value.UTC()
		value = &utc
	}
	return value, nil
}
