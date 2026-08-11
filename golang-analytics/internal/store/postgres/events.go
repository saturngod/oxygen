package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"oxygen/analytics/internal/domain"
)

func (s *Store) IngestBatch(ctx context.Context, events []domain.Event) (domain.IngestResult, error) {
	if len(events) == 0 {
		return domain.IngestResult{}, nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.IngestResult{}, fmt.Errorf("begin analytics ingest: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := domain.IngestResult{}
	for _, event := range events {
		accepted, err := insertEvent(ctx, tx, event)
		if err != nil {
			return domain.IngestResult{}, err
		}
		if !accepted {
			result.Duplicates++
			continue
		}

		if err := applyEvent(ctx, tx, event); err != nil {
			return domain.IngestResult{}, err
		}
		result.Accepted++
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.IngestResult{}, fmt.Errorf("commit analytics ingest: %w", err)
	}

	return result, nil
}

func insertEvent(ctx context.Context, tx pgx.Tx, event domain.Event) (bool, error) {
	var acceptedID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO analytics_events (
			event_id, event_type, schema_version, sequence,
			organization_id, live_stream_id, live_stream_session_id,
			occurred_at, current_viewers, interval_peak_viewers, session_peak_viewers,
			viewer_identity_additions, playlist_requests_delta, segment_requests_delta,
			unique_viewers_total, playlist_requests_total, segment_requests_total
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
		ON CONFLICT DO NOTHING
		RETURNING event_id
	`, event.EventID, event.EventType, event.SchemaVersion, event.Sequence,
		event.OrganizationID, event.LiveStreamID, event.SessionID,
		event.OccurredAt.UTC(), event.CurrentViewers, event.IntervalPeak, event.SessionPeak,
		event.IdentityAdditions, event.PlaylistDelta, event.SegmentDelta,
		event.UniqueTotal, event.PlaylistTotal, event.SegmentTotal,
	).Scan(&acceptedID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert analytics event: %w", err)
	}

	return acceptedID != uuid.Nil, nil
}

func applyEvent(ctx context.Context, tx pgx.Tx, event domain.Event) error {
	if err := updateCoverage(ctx, tx, event.OccurredAt); err != nil {
		return err
	}

	switch event.EventType {
	case domain.EventSessionStarted:
		return upsertStartedSession(ctx, tx, event)
	case domain.EventViewerSample:
		if err := ensureSession(ctx, tx, event); err != nil {
			return err
		}
		if err := upsertHourly(ctx, tx, event); err != nil {
			return err
		}
		return updateSessionMetrics(ctx, tx, event, "live")
	case domain.EventSessionEnded, domain.EventSessionFailed:
		if err := ensureSession(ctx, tx, event); err != nil {
			return err
		}
		if err := upsertHourly(ctx, tx, event); err != nil {
			return err
		}
		status := "ended"
		if event.EventType == domain.EventSessionFailed {
			status = "failed"
		}
		return updateSessionMetrics(ctx, tx, event, status)
	default:
		return fmt.Errorf("unsupported event type %q", event.EventType)
	}
}

func updateCoverage(ctx context.Context, tx pgx.Tx, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE analytics_service_state
		SET coverage_started_at = LEAST(COALESCE(coverage_started_at, $1), $1), updated_at = now()
		WHERE id = 1
	`, occurredAt.UTC())
	if err != nil {
		return fmt.Errorf("update analytics coverage: %w", err)
	}
	return nil
}

func upsertStartedSession(ctx context.Context, tx pgx.Tx, event domain.Event) error {
	startedAt := event.OccurredAt.UTC()
	if event.StartedAt != nil {
		startedAt = event.StartedAt.UTC()
	}
	status := event.Status
	if status == "" {
		status = "live"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO session_metrics (
			live_stream_session_id, organization_id, live_stream_id, status, started_at, last_sequence, last_event_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (live_stream_session_id) DO UPDATE SET
			organization_id = EXCLUDED.organization_id,
			live_stream_id = EXCLUDED.live_stream_id,
			started_at = COALESCE(session_metrics.started_at, EXCLUDED.started_at),
			last_sequence = GREATEST(session_metrics.last_sequence, EXCLUDED.last_sequence),
			last_event_at = GREATEST(session_metrics.last_event_at, EXCLUDED.last_event_at),
			updated_at = now()
		WHERE session_metrics.organization_id = EXCLUDED.organization_id
		  AND session_metrics.live_stream_id = EXCLUDED.live_stream_id
	`, event.SessionID, event.OrganizationID, event.LiveStreamID, status, startedAt, event.Sequence, event.OccurredAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert started session: %w", err)
	}
	return nil
}

func ensureSession(ctx context.Context, tx pgx.Tx, event domain.Event) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO session_metrics (live_stream_session_id, organization_id, live_stream_id, status, started_at)
		VALUES ($1, $2, $3, 'live', $4)
		ON CONFLICT (live_stream_session_id) DO NOTHING
	`, event.SessionID, event.OrganizationID, event.LiveStreamID, event.OccurredAt.UTC())
	if err != nil {
		return fmt.Errorf("ensure session metrics: %w", err)
	}
	return nil
}

func upsertHourly(ctx context.Context, tx pgx.Tx, event domain.Event) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO stream_hourly_metrics (
			id, organization_id, live_stream_id, bucket_start,
			peak_viewers, viewer_identity_additions, playlist_requests, segment_requests, sample_count
		) VALUES (
			$1, $2, $3, date_trunc('hour', $4 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
			$5, $6, $7, $8, 1
		)
		ON CONFLICT (live_stream_id, bucket_start) DO UPDATE SET
			peak_viewers = GREATEST(stream_hourly_metrics.peak_viewers, EXCLUDED.peak_viewers),
			viewer_identity_additions = stream_hourly_metrics.viewer_identity_additions + EXCLUDED.viewer_identity_additions,
			playlist_requests = stream_hourly_metrics.playlist_requests + EXCLUDED.playlist_requests,
			segment_requests = stream_hourly_metrics.segment_requests + EXCLUDED.segment_requests,
			sample_count = stream_hourly_metrics.sample_count + EXCLUDED.sample_count,
			updated_at = now()
		WHERE stream_hourly_metrics.organization_id = EXCLUDED.organization_id
	`, uuid.New(), event.OrganizationID, event.LiveStreamID, event.OccurredAt.UTC(), max(event.IntervalPeak, event.CurrentViewers), event.IdentityAdditions, event.PlaylistDelta, event.SegmentDelta)
	if err != nil {
		return fmt.Errorf("upsert hourly metric: %w", err)
	}
	return nil
}

func updateSessionMetrics(ctx context.Context, tx pgx.Tx, event domain.Event, terminalStatus string) error {
	current := event.CurrentViewers
	endedAt := event.EndedAt
	if terminalStatus == "ended" || terminalStatus == "failed" {
		current = 0
		if endedAt == nil {
			endedAt = &event.OccurredAt
		}
	}

	_, err := tx.Exec(ctx, `
		UPDATE session_metrics
		SET current_viewers = CASE WHEN $2 > last_sequence THEN $3 ELSE current_viewers END,
			peak_viewers = GREATEST(peak_viewers, $4, $5),
			unique_viewers = GREATEST(unique_viewers, $6),
			playlist_requests = GREATEST(playlist_requests, $7),
			segment_requests = GREATEST(segment_requests, $8),
			status = CASE WHEN status IN ('ended', 'failed') THEN status ELSE $9 END,
			ended_at = CASE WHEN $10::timestamptz IS NULL THEN ended_at ELSE COALESCE(ended_at, $10) END,
			last_sequence = GREATEST(last_sequence, $2),
			last_event_at = GREATEST(last_event_at, $11),
			updated_at = now()
		WHERE live_stream_session_id = $1
		  AND organization_id = $12
		  AND live_stream_id = $13
	`, event.SessionID, event.Sequence, current, event.SessionPeak, event.IntervalPeak,
		event.UniqueTotal, event.PlaylistTotal, event.SegmentTotal, terminalStatus, endedAt, event.OccurredAt.UTC(), event.OrganizationID, event.LiveStreamID)
	if err != nil {
		return fmt.Errorf("update session metrics: %w", err)
	}
	return nil
}
