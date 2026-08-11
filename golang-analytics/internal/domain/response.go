package domain

import "time"

type Point struct {
	Timestamp time.Time `json:"timestamp"`
	Value     *int      `json:"value"`
	Complete  bool      `json:"complete"`
}

type Summary struct {
	PeakViewers             int   `json:"peak_viewers"`
	Broadcasts              int64 `json:"broadcasts"`
	ViewerIdentityAdditions int64 `json:"viewer_identity_additions"`
	PlaybackRequests        int64 `json:"playback_requests"`
}

type AnalyticsResponse struct {
	RangeStart     time.Time  `json:"range_start"`
	RangeEnd       time.Time  `json:"range_end"`
	CoverageStart  *time.Time `json:"coverage_start"`
	Timezone       string     `json:"timezone"`
	Granularity    string     `json:"granularity"`
	Points         []Point    `json:"points"`
	Summary        Summary    `json:"summary"`
	GeneratedAt    time.Time  `json:"generated_at"`
	DataLagSeconds int64      `json:"data_lag_seconds"`
}
