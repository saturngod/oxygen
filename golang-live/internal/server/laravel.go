package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"oxygen/live/internal/config"
)

type deliveryHTTPError struct {
	status int
	body   string
}

func (e *deliveryHTTPError) Error() string {
	return fmt.Sprintf("delivery returned %d: %s", e.status, e.body)
}

func isPermanentDeliveryError(err error) bool {
	var statusErr *deliveryHTTPError
	if !errors.As(err, &statusErr) {
		return false
	}

	switch statusErr.status {
	case http.StatusBadRequest,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

type LaravelClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewLaravelClient(cfg config.Config) *LaravelClient {
	return &LaravelClient{
		baseURL: cfg.LaravelURL,
		token:   cfg.ServiceToken,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

const maxPostAttempts = 3

func (c *LaravelClient) Post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error

	for attempt := 0; attempt < maxPostAttempts; attempt++ {
		if attempt > 0 {
			// Linear backoff; abort early if the caller's context is cancelled.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}

		retry, err := c.doPost(ctx, path, body, out)
		if err == nil {
			return nil
		}

		lastErr = err
		if !retry {
			// The durable outbox decides whether a 4xx response is safe to
			// quarantine or should remain queued for a later deployment/token.
			return err
		}
	}

	return lastErr
}

// doPost performs a single attempt. The bool reports whether the error is
// transient (network failure or 5xx) and therefore worth retrying.
func (c *LaravelClient) doPost(ctx context.Context, path string, body []byte, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Live-Service-Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return true, fmt.Errorf("post %s: %w", path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return true, fmt.Errorf("post %s returned %d: %s", path, resp.StatusCode, string(b))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, fmt.Errorf("post %s: %w", path, &deliveryHTTPError{status: resp.StatusCode, body: string(b)})
	}

	if out == nil {
		return false, nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, fmt.Errorf("decode %s response: %w", path, err)
	}

	return false, nil
}

type AuthPublishRequest struct {
	PublicID  string `json:"public_id"`
	StreamKey string `json:"stream_key"`
}

type AuthPublishResponse struct {
	Allowed bool `json:"allowed"`
	Stream  struct {
		ID                         string   `json:"id"`
		OrganizationID             string   `json:"organization_id"`
		PublicID                   string   `json:"public_id"`
		SettingsVersion            int      `json:"settings_version"`
		HLSURL                     string   `json:"hls_url"`
		Qualities                  []string `json:"qualities"`
		LiveSegmentDurationSeconds int      `json:"live_segment_duration_seconds"`
	} `json:"stream"`
}

type SessionStartedRequest struct {
	PublicID   string `json:"public_id"`
	ExternalID string `json:"external_id,omitempty"`
	HLSURL     string `json:"hls_url,omitempty"`
	HLSPrefix  string `json:"hls_prefix,omitempty"`
}

type SessionStartedResponse struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"session_id"`
}

type ViewerSnapshotRequest struct {
	PublicID         string `json:"public_id"`
	SessionID        string `json:"session_id"`
	Minute           string `json:"minute"`
	CurrentViewers   int    `json:"current_viewers"`
	UniqueViewers    int    `json:"unique_viewers_seen"`
	PlaylistRequests int64  `json:"playlist_requests"`
	SegmentRequests  int64  `json:"segment_requests"`
}
