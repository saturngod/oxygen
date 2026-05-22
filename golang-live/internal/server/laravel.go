package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"oxygen/live/internal/config"
)

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

func (c *LaravelClient) Post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Live-Service-Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("post %s returned %d: %s", path, resp.StatusCode, string(b))
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

type AuthPublishRequest struct {
	PublicID  string `json:"public_id"`
	StreamKey string `json:"stream_key"`
}

type AuthPublishResponse struct {
	Allowed bool `json:"allowed"`
	Stream  struct {
		ID               string `json:"id"`
		OrganizationID   string `json:"organization_id"`
		PublicID         string `json:"public_id"`
		SettingsVersion  int    `json:"settings_version"`
		RecordingEnabled bool   `json:"recording_enabled"`
		HLSURL           string `json:"hls_url"`
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
