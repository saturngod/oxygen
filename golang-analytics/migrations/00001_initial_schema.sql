-- +goose Up

CREATE TABLE analytics_service_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    coverage_started_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO analytics_service_state (id) VALUES (1);

CREATE TABLE analytics_events (
    event_id UUID PRIMARY KEY,
    event_type VARCHAR(64) NOT NULL,
    schema_version SMALLINT NOT NULL,
    sequence BIGINT NOT NULL,
    organization_id UUID NOT NULL,
    live_stream_id UUID NOT NULL,
    live_stream_session_id UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    current_viewers INTEGER NOT NULL DEFAULT 0,
    interval_peak_viewers INTEGER NOT NULL DEFAULT 0,
    session_peak_viewers INTEGER NOT NULL DEFAULT 0,
    viewer_identity_additions BIGINT NOT NULL DEFAULT 0,
    playlist_requests_delta BIGINT NOT NULL DEFAULT 0,
    segment_requests_delta BIGINT NOT NULL DEFAULT 0,
    unique_viewers_total BIGINT NOT NULL DEFAULT 0,
    playlist_requests_total BIGINT NOT NULL DEFAULT 0,
    segment_requests_total BIGINT NOT NULL DEFAULT 0,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT analytics_events_session_sequence_unique UNIQUE (live_stream_session_id, sequence),
    CONSTRAINT analytics_events_non_negative CHECK (
        current_viewers >= 0
        AND interval_peak_viewers >= 0
        AND session_peak_viewers >= 0
        AND viewer_identity_additions >= 0
        AND playlist_requests_delta >= 0
        AND segment_requests_delta >= 0
        AND unique_viewers_total >= 0
        AND playlist_requests_total >= 0
        AND segment_requests_total >= 0
    )
);

CREATE INDEX analytics_events_stream_occurred_index
    ON analytics_events (live_stream_id, occurred_at);
CREATE INDEX analytics_events_organization_occurred_index
    ON analytics_events (organization_id, occurred_at);
CREATE INDEX analytics_events_ingested_index
    ON analytics_events (ingested_at);

CREATE TABLE stream_hourly_metrics (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    live_stream_id UUID NOT NULL,
    bucket_start TIMESTAMPTZ NOT NULL,
    peak_viewers INTEGER NOT NULL DEFAULT 0,
    viewer_identity_additions BIGINT NOT NULL DEFAULT 0,
    playlist_requests BIGINT NOT NULL DEFAULT 0,
    segment_requests BIGINT NOT NULL DEFAULT 0,
    sample_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT stream_hourly_metrics_stream_bucket_unique UNIQUE (live_stream_id, bucket_start)
);

CREATE INDEX stream_hourly_metrics_organization_bucket_index
    ON stream_hourly_metrics (organization_id, bucket_start);

CREATE TABLE session_metrics (
    live_stream_session_id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    live_stream_id UUID NOT NULL,
    status VARCHAR(32) NOT NULL,
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    current_viewers INTEGER NOT NULL DEFAULT 0,
    peak_viewers INTEGER NOT NULL DEFAULT 0,
    unique_viewers BIGINT NOT NULL DEFAULT 0,
    playlist_requests BIGINT NOT NULL DEFAULT 0,
    segment_requests BIGINT NOT NULL DEFAULT 0,
    last_sequence BIGINT NOT NULL DEFAULT 0,
    last_event_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX session_metrics_stream_started_index
    ON session_metrics (live_stream_id, started_at);
CREATE INDEX session_metrics_organization_started_index
    ON session_metrics (organization_id, started_at);
CREATE INDEX session_metrics_active_stream_index
    ON session_metrics (live_stream_id, status)
    WHERE status IN ('starting', 'live');

-- +goose Down

DROP TABLE IF EXISTS session_metrics;
DROP TABLE IF EXISTS stream_hourly_metrics;
DROP TABLE IF EXISTS analytics_events;
DROP TABLE IF EXISTS analytics_service_state;
