package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const maintenanceLockKey int64 = 0x6f786967656e616c

func (s *Store) Reconcile(ctx context.Context, start, end time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin analytics reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, maintenanceLockKey).Scan(&locked); err != nil {
		return fmt.Errorf("acquire analytics reconciliation lock: %w", err)
	}
	if !locked {
		return nil
	}

	rows, err := tx.Query(ctx, `
		SELECT organization_id, live_stream_id, occurred_at,
		       interval_peak_viewers, current_viewers,
		       viewer_identity_additions, playlist_requests_delta,
		       segment_requests_delta
		FROM analytics_events
		WHERE occurred_at >= $1 AND occurred_at < $2
		  AND event_type IN ('viewer.sample.v1', 'session.ended.v1', 'session.failed.v1')
		ORDER BY occurred_at, live_stream_id, sequence
	`, start.UTC(), end.UTC())
	if err != nil {
		return fmt.Errorf("read analytics reconciliation events: %w", err)
	}

	type aggregate struct {
		organizationID uuid.UUID
		streamID       uuid.UUID
		bucket         time.Time
		peak           int
		identities     int64
		playlist       int64
		segment        int64
		samples        int
	}
	aggregates := make(map[string]*aggregate)
	for rows.Next() {
		var organizationID, streamID uuid.UUID
		var occurredAt time.Time
		var intervalPeak, current int
		var identities, playlist, segment int64
		if err := rows.Scan(&organizationID, &streamID, &occurredAt, &intervalPeak, &current, &identities, &playlist, &segment); err != nil {
			rows.Close()
			return fmt.Errorf("scan analytics reconciliation event: %w", err)
		}
		bucket := occurredAt.UTC().Truncate(time.Hour)
		key := streamID.String() + ":" + bucket.Format(time.RFC3339)
		item := aggregates[key]
		if item == nil {
			item = &aggregate{organizationID: organizationID, streamID: streamID, bucket: bucket}
			aggregates[key] = item
		}
		if peak := max(intervalPeak, current); peak > item.peak {
			item.peak = peak
		}
		item.identities += identities
		item.playlist += playlist
		item.segment += segment
		item.samples++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate analytics reconciliation events: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM stream_hourly_metrics
		WHERE bucket_start >= $1 AND bucket_start < $2
	`, start.UTC().Truncate(time.Hour), end.UTC().Truncate(time.Hour)); err != nil {
		return fmt.Errorf("clear analytics reconciliation buckets: %w", err)
	}
	for _, item := range aggregates {
		if _, err := tx.Exec(ctx, `
			INSERT INTO stream_hourly_metrics (
				id, organization_id, live_stream_id, bucket_start, peak_viewers,
				viewer_identity_additions, playlist_requests, segment_requests, sample_count
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, uuid.New(), item.organizationID, item.streamID, item.bucket, item.peak, item.identities, item.playlist, item.segment, item.samples); err != nil {
			return fmt.Errorf("write analytics reconciliation bucket: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit analytics reconciliation: %w", err)
	}
	return nil
}

func (s *Store) PruneEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	connection, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire analytics pruning connection: %w", err)
	}
	defer connection.Release()
	var locked bool
	if err := connection.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, maintenanceLockKey).Scan(&locked); err != nil {
		return 0, fmt.Errorf("acquire analytics pruning lock: %w", err)
	}
	if !locked {
		return 0, nil
	}
	defer func() {
		_, _ = connection.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, maintenanceLockKey)
	}()

	var deleted int64
	for {
		result, err := connection.Exec(ctx, `
			WITH doomed AS (
				SELECT event_id FROM analytics_events
				WHERE occurred_at < $1
				ORDER BY occurred_at
				LIMIT 5000
			)
			DELETE FROM analytics_events
			WHERE event_id IN (SELECT event_id FROM doomed)
		`, cutoff.UTC())
		if err != nil {
			return deleted, fmt.Errorf("prune analytics events: %w", err)
		}
		count := result.RowsAffected()
		deleted += count
		if count == 0 {
			return deleted, nil
		}
	}
}

func max(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}
