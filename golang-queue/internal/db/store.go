package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MediaFileRow struct {
	ID             string
	OrganizationID string
	FolderID       *string
	Title          string
	FileName       *string
	FilePath       *string
	SourceURL      *string
	StreamingURL   *string
	Size           int64
	Status         string
	Progress       int
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

type MediaFileProfileRow struct {
	ID          string
	MediaFileID string
	ProfileID   *string
	Name        string
	Qualities   []string
}

type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{
		pool: pool,
		log:  slog.With("component", "db"),
	}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) LoadMediaFile(ctx context.Context, id, organizationID string) (*MediaFileRow, error) {
	const q = `
		SELECT id, organization_id, folder_id, title, file_name, file_path,
		       source_url, streaming_url, size, status, progress, created_at, updated_at
		FROM media_files
		WHERE id = $1 AND organization_id = $2
	`

	row := s.pool.QueryRow(ctx, q, id, organizationID)
	var m MediaFileRow
	var qualitiesJSON []byte

	if err := row.Scan(
		&m.ID, &m.OrganizationID, &m.FolderID, &m.Title, &m.FileName, &m.FilePath,
		&m.SourceURL, &m.StreamingURL, &m.Size, &m.Status, &m.Progress,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan media_file: %w", err)
	}

	_ = qualitiesJSON
	return &m, nil
}

func (s *Store) LoadMediaFileProfile(ctx context.Context, mediaFileID string) (*MediaFileProfileRow, error) {
	const q = `
		SELECT id, media_file_id, profile_id, name, qualities
		FROM media_file_profiles
		WHERE media_file_id = $1
		LIMIT 1
	`

	row := s.pool.QueryRow(ctx, q, mediaFileID)
	var p MediaFileProfileRow
	var qualitiesJSON []byte

	if err := row.Scan(&p.ID, &p.MediaFileID, &p.ProfileID, &p.Name, &qualitiesJSON); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan media_file_profile: %w", err)
	}

	if err := json.Unmarshal(qualitiesJSON, &p.Qualities); err != nil {
		return nil, fmt.Errorf("decode qualities json: %w", err)
	}

	return &p, nil
}

func (s *Store) UpdateProgress(ctx context.Context, id string, status string, progress int) error {
	progress = clamp(progress, 0, 100)
	const q = `UPDATE media_files SET status = $2, progress = $3, updated_at = now() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, status, progress)
	return err
}

func (s *Store) UpdateSuccess(ctx context.Context, id, streamingURL string) error {
	const q = `UPDATE media_files SET status = 'success', progress = 100, streaming_url = $2, updated_at = now() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, streamingURL)
	return err
}

func (s *Store) UpdateFailed(ctx context.Context, id string, progress int) error {
	progress = clamp(progress, 0, 100)
	const q = `UPDATE media_files SET status = 'failed', progress = $2, updated_at = now() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, progress)
	return err
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
