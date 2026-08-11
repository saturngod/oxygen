package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"oxygen/live/internal/config"
)

type AnalyticsEventType string

const (
	AnalyticsSessionStarted AnalyticsEventType = "session.started.v1"
	AnalyticsViewerSample   AnalyticsEventType = "viewer.sample.v1"
	AnalyticsSessionEnded   AnalyticsEventType = "session.ended.v1"
	AnalyticsSessionFailed  AnalyticsEventType = "session.failed.v1"
)

type AnalyticsEvent struct {
	EventID           string             `json:"event_id"`
	EventType         AnalyticsEventType `json:"event_type"`
	SchemaVersion     int                `json:"schema_version"`
	Sequence          int64              `json:"sequence"`
	OccurredAt        time.Time          `json:"occurred_at"`
	OrganizationID    string             `json:"organization_id"`
	LiveStreamID      string             `json:"live_stream_id"`
	SessionID         string             `json:"live_stream_session_id"`
	Status            string             `json:"status,omitempty"`
	CurrentViewers    int                `json:"current_viewers"`
	IntervalPeak      int                `json:"interval_peak_viewers"`
	SessionPeak       int                `json:"session_peak_viewers"`
	IdentityAdditions int64              `json:"viewer_identity_additions"`
	PlaylistDelta     int64              `json:"playlist_requests_delta"`
	SegmentDelta      int64              `json:"segment_requests_delta"`
	UniqueTotal       int64              `json:"unique_viewers_total"`
	PlaylistTotal     int64              `json:"playlist_requests_total"`
	SegmentTotal      int64              `json:"segment_requests_total"`
	StartedAt         *time.Time         `json:"started_at,omitempty"`
	EndedAt           *time.Time         `json:"ended_at,omitempty"`
}

type AnalyticsEventBatch struct {
	Events []AnalyticsEvent `json:"events"`
}

type AnalyticsClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewAnalyticsClient(cfg config.Config) *AnalyticsClient {
	return &AnalyticsClient{
		baseURL: strings.TrimRight(cfg.AnalyticsURL, "/"),
		token:   cfg.AnalyticsToken,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *AnalyticsClient) PostBatch(ctx context.Context, batch AnalyticsEventBatch) error {
	if c.baseURL == "" || c.token == "" {
		return fmt.Errorf("analytics client is not configured")
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal analytics batch: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		retry, err := c.doPost(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			return err
		}
	}
	return lastErr
}

func (c *AnalyticsClient) doPost(ctx context.Context, body []byte) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/events/batch", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build analytics request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return true, fmt.Errorf("post analytics batch: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode >= 500 {
		return true, fmt.Errorf("analytics returned %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("analytics returned %d", response.StatusCode)
	}
	return false, nil
}
