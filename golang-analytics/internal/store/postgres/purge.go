package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PurgeStream(ctx context.Context, organizationID, streamID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin analytics purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO deleted_live_streams (organization_id, live_stream_id)
		VALUES ($1, $2)
		ON CONFLICT (organization_id, live_stream_id) DO NOTHING
	`, organizationID, streamID); err != nil {
		return fmt.Errorf("tombstone deleted live stream: %w", err)
	}

	for _, table := range []string{"analytics_events", "stream_hourly_metrics", "session_metrics"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE organization_id = $1 AND live_stream_id = $2", organizationID, streamID); err != nil {
			return fmt.Errorf("purge %s: %w", table, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit analytics purge: %w", err)
	}

	return nil
}
