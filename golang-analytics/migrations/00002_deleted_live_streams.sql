-- +goose Up

CREATE TABLE deleted_live_streams (
    organization_id UUID NOT NULL,
    live_stream_id UUID NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, live_stream_id)
);

-- +goose Down

DROP TABLE IF EXISTS deleted_live_streams;
