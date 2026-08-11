# Analytics Service Plan

## Scope and decisions

Move viewer analytics out of Laravel’s transactional database into an independently scalable Go service with its own PostgreSQL database.

“No migration” means no historical-data backfill from Laravel. The new database still gets a versioned initial schema. Data before the service cutover is unavailable, not zero.

- New module: golang-analytics.
- Private HTTP API on port 8090.
- Laravel remains the browser-facing authorization gateway.
- golang-live produces events through a durable disk outbox.
- Raw events are retained for 30 days; hourly and session aggregates are permanent.
- No Kafka initially; the delivery interface remains replaceable.
- Analytics runs in its own container, not under Supervisor.

### Capacity baseline

At the default 15-second sample interval, four continuously running channels
produce about 23,040 raw viewer events per day (691,200 events in a 30-day raw
retention window) and 35,040 durable hourly stream buckets per year. Raw event
storage therefore grows only with the configured retention window; hourly and
session tables grow predictably with streams and broadcasts, not with every
viewer request. A year-range chart reads at most 12 monthly buckets per stream.
Scale `analytics-api` horizontally and size the dedicated PostgreSQL pool/storage
independently; keep the raw-event retention and reconciliation lookback
configurable as traffic grows.

~~~text
OBS -> golang-live -> durable analytics outbox -> golang-analytics -> analytics PostgreSQL
                         |
                         +-> existing Laravel lifecycle callbacks

Browser -> Laravel authorization/UI -> private analytics query API
~~~

## New Go module

Create:

~~~text
golang-analytics/
├── cmd/analytics/main.go
├── internal/
│   ├── aggregate/{service.go,service_test.go}
│   ├── auth/{middleware.go,middleware_test.go}
│   ├── config/{config.go,config_test.go}
│   ├── domain/{event.go,period.go,response.go}
│   ├── httpapi/{router.go,health_handler.go,ingest_handler.go,analytics_handler.go,live_handler.go,handlers_test.go}
│   ├── maintenance/{scheduler.go,reconciler.go,retention.go}
│   ├── query/{service.go,service_test.go}
│   └── store/{store.go,postgres/}
├── migrations/00001_initial_schema.sql
├── .env.example
├── go.mod
├── go.sum
└── README.md
~~~

Use net/http, slog, pgx/v5, google/uuid, and pressly/goose/v3. The binary supports serve, migrate, and reconcile --hours=48.

## Configuration

internal/config/config.go should validate:

~~~text
ANALYTICS_ADDR=:8090
ANALYTICS_DATABASE_URL=
ANALYTICS_INGEST_TOKEN=
ANALYTICS_QUERY_TOKEN=
ANALYTICS_RAW_RETENTION_DAYS=30
ANALYTICS_RECONCILIATION_HOURS=48
ANALYTICS_DB_MAX_CONNECTIONS=20
ANALYTICS_DB_MIN_CONNECTIONS=2
ANALYTICS_MAXIMUM_BATCH_SIZE=500
ANALYTICS_MAXIMUM_REQUEST_BODY_BYTES=2097152
ANALYTICS_HTTP_READ_TIMEOUT_SECONDS=10
ANALYTICS_HTTP_WRITE_TIMEOUT_SECONDS=15
ANALYTICS_HTTP_IDLE_TIMEOUT_SECONDS=75
ANALYTICS_SHUTDOWN_TIMEOUT_SECONDS=15
~~~

Set every PostgreSQL connection to UTC with SET TIME ZONE 'UTC'. Reject missing tokens, invalid URLs, non-positive limits, and identical ingest/query tokens in production.

## Event contract

Supported events:

- session.started.v1
- viewer.sample.v1
- session.ended.v1
- session.failed.v1

Implement internal/domain/event.go with:

~~~go
type Event struct {
    EventID           uuid.UUID
    EventType         EventType
    SchemaVersion     int
    Sequence          int64
    OccurredAt        time.Time
    OrganizationID    uuid.UUID
    LiveStreamID      uuid.UUID
    SessionID         uuid.UUID
    CurrentViewers    int
    IntervalPeak      int
    SessionPeak       int
    IdentityAdditions int64
    PlaylistDelta     int64
    SegmentDelta      int64
    UniqueTotal       int64
    PlaylistTotal     int64
    SegmentTotal      int64
}
~~~

event_id is the retry key and sequence is monotonic per session. current_viewers is the latest sample. interval_peak_viewers is the peak since the previous emitted event. Identity and request values are interval deltas; cumulative totals are retained for reconciliation. No viewer fingerprint, IP, or user agent leaves golang-live.

## PostgreSQL schema

Create migrations/00001_initial_schema.sql managed by Goose. Do not create foreign keys into Laravel’s database; IDs are opaque UUIDs.

### analytics_service_state

Singleton row with coverage_started_at, created_at, and updated_at. coverage_started_at is permanent and must not move when raw events expire.

### analytics_events

Columns:

- event_id UUID primary key
- event_type, schema_version, sequence
- organization_id, live_stream_id, live_stream_session_id
- occurred_at, ingested_at
- current_viewers, interval_peak_viewers, session_peak_viewers
- viewer_identity_additions, playlist_requests_delta, segment_requests_delta
- unique_viewers_total, playlist_requests_total, segment_requests_total

Constraints/indexes:

- Unique live_stream_session_id plus sequence.
- Index live_stream_id plus occurred_at.
- Index organization_id plus occurred_at.
- Index ingested_at.
- All counters non-negative.

### stream_hourly_metrics

Columns:

- UUID id
- organization_id, live_stream_id
- UTC bucket_start
- peak_viewers
- viewer_identity_additions
- playlist_requests
- segment_requests
- sample_count
- created_at, updated_at

Unique live_stream_id plus bucket_start. Index organization_id plus bucket_start.

### session_metrics

Columns:

- live_stream_session_id UUID primary key
- organization_id, live_stream_id
- status, started_at, ended_at
- current_viewers, peak_viewers, unique_viewers
- playlist_requests, segment_requests
- last_sequence, last_event_at
- created_at, updated_at

Indexes: live_stream_id plus started_at, organization_id plus started_at, and a partial active-session index on live_stream_id plus status.

## Ingestion code

Expose POST /internal/v1/events/batch. internal/httpapi/ingest_handler.go authenticates the ingest token, limits the body and batch size, validates every event, and calls EventStore.IngestBatch.

store/postgres/events.go uses one transaction per batch:

1. Insert each event with ON CONFLICT DO NOTHING.
2. Ignore duplicate events without reapplying metrics.
3. Update session_metrics monotonically.
4. Upsert the UTC hourly bucket.
5. Set coverage metadata for the first accepted event.
6. Commit the batch.

Hourly rules: peak uses GREATEST, identity/request deltas are summed, and sample count increments. Session current viewers update only for a newer sequence; peak and cumulative totals use GREATEST; terminal events set current viewers to zero and cannot reopen a session.

Return 202 with accepted and duplicates. Return 400, 401, 413, 422, and 503 for malformed, unauthorized, oversized, invalid, and unavailable requests.

## Query API

Expose:

~~~text
GET /internal/v1/organizations/{organizationID}/streams/{streamID}/analytics?period=day|month|year
GET /internal/v1/organizations/{organizationID}/streams/{streamID}/live
GET /healthz
GET /readyz
~~~

internal/query/service.go loads hourly rows, creates expected buckets, aggregates in Go, and counts overlapping sessions with:

~~~sql
started_at <= range_end
AND (ended_at IS NULL OR ended_at >= range_start)
~~~

Response fields:

~~~text
range_start
range_end
coverage_start
timezone
granularity
points: timestamp, nullable value, complete
summary: peak_viewers, broadcasts, viewer_identity_additions, playback_requests
generated_at
data_lag_seconds
~~~

A null point means before analytics coverage. Zero means a covered inactive bucket. The current bucket is complete=false. Day means 24 UTC hours, month means 30 UTC days, and year means 12 UTC months.

## Maintenance

Run reconciliation hourly with a 48-hour lookback under a PostgreSQL advisory lock:

1. Read retained events.
2. Rebuild affected hourly buckets from scratch.
3. Replace buckets transactionally.
4. Release the lock.

Run raw-event retention daily under the same lock. Delete analytics_events in batches older than the cutoff. Never delete hourly or session metrics.

## golang-live changes

Add AnalyticsURL, AnalyticsToken, AnalyticsOutboxRoot, and AnalyticsBatchSize to internal/config/config.go.

Create internal/server/analytics.go with an HTTP client that retries network errors and 5xx responses but not permanent 4xx responses.

Refactor the existing callback file outbox into a reusable FileOutbox with a destination interface. Use separate roots:

~~~text
/var/lib/oxygen-live/callbacks
/var/lib/oxygen-live/analytics-outbox
~~~

Each file is written to a temporary file, synced, atomically renamed, delivered asynchronously, and deleted only after 2xx.

Extend tracker state with organization ID, stream ID, session ID, identity/request deltas, interval peak, and analytics sequence. Add PrepareAnalyticsBatch and AcknowledgeAnalyticsBatch. Acknowledge counters only after the batch is durably written locally. This prevents analytics downtime from interrupting RTMP/HLS.

Copy organization and stream IDs from the Laravel publish-auth response. Emit start, sample, and terminal events. Persist the final analytics event before deleting tracker state.

## Laravel changes

Create:

~~~text
app/Contracts/LiveStreamAnalyticsReader.php
app/Services/RemoteLiveStreamAnalytics.php
~~~

The contract exposes analytics for a period and current live metrics for an organization/stream. The remote client uses services.analytics.url and services.analytics.query_token, short connect/request timeouts, bounded retries, and an unavailable result instead of a 500.

Update the viewer controller, live-detail metrics, and React point type to support number or null plus complete boolean. Never convert null into zero. Do not expose service tokens through Inertia.

During verification, local Laravel analytics may remain temporarily for comparison. After cutover, stop Laravel viewer-snapshot metric persistence and local compaction while retaining lifecycle callbacks.

## Dockerfile changes

Update Dockerfile:

1. Add an analytics-build Go stage.
2. Add an analytics-runtime stage containing only the static analytics binary, CA certificates, healthcheck tooling, and a non-root user.
3. Keep the current runtime stage as the default/final stage.
4. Do not copy the analytics binary into the existing Oxygen runtime.
5. Do not add analytics to Supervisor.

The new target must build with:

~~~text
docker build --target analytics-runtime -t oxygen-analytics .
~~~

The existing runtime also gets ANALYTICS_OUTBOX_ROOT=/var/lib/oxygen-live/analytics-outbox, directory creation/ownership, and a volume declaration.

## Compose changes

Update compose.yaml with analytics-postgres and analytics-api. The analytics
API applies pending Goose migrations during its own startup before accepting
traffic, so a separate migration container is not required for a fresh
installation.

analytics-postgres uses database oxygen_analytics, user oxygen_analytics, a dedicated analytics-pgdata volume, no published PostgreSQL port, and a pg_isready healthcheck.

analytics-api builds analytics-runtime, runs oxygen-analytics serve, applies
pending migrations once, exposes internal port 8090, depends on PostgreSQL
health, and restarts on failure. The standalone `oxygen-analytics migrate`
command remains available for manual/operator migration runs.

Add analytics-outbox to the existing oxygen service volumes. Add analytics-outbox and analytics-pgdata to Compose volumes. Do not make oxygen depend on analytics health; the outbox must allow streaming during analytics downtime.

Do not publish analytics HTTP in production. For local debugging only, optionally map 127.0.0.1:8090:8090. Internal URL: http://analytics-api:8090.

## Environment updates

Add to .env.example, docker/local.env.example, Laravel services configuration, and Go documentation:

~~~env
ANALYTICS_URL=http://analytics-api:8090
ANALYTICS_DATABASE_URL=postgres://oxygen_analytics:local-analytics-password@analytics-postgres:5432/oxygen_analytics?sslmode=disable
ANALYTICS_INGEST_TOKEN=local-analytics-ingest-token-change-before-production
ANALYTICS_QUERY_TOKEN=local-analytics-query-token-change-before-production
ANALYTICS_OUTBOX_ROOT=/var/lib/oxygen-live/analytics-outbox
ANALYTICS_RAW_RETENTION_DAYS=30
ANALYTICS_RECONCILIATION_HOURS=48
~~~

Use different production tokens for ingestion and queries. Neither token reaches the browser.

## Testing

Analytics Go tests cover authentication, malformed/oversized batches, duplicate event IDs, duplicate sequences, out-of-order events, multiple sessions per hour, UTC boundaries, day/month/year aggregation, session overlap, tenant scoping, cutover coverage, reconciliation idempotency, retention, and database restart.

golang-live tests cover delta calculation, interval peak reset, durable batch writes, retry/restart replay, short-session terminal events, analytics outage behavior, and outbox disk failures.

Laravel tests cover remote response contracts, organization authorization, stream scoping, timeout/unavailable behavior, nullable points, and absence of analytics credentials in Inertia props.

Run:

~~~text
cd golang-analytics
go test ./...
go test -race ./...
~~~

Docker verification:

~~~text
docker compose config
docker compose build oxygen analytics-api
docker compose up -d analytics-postgres analytics-api
docker compose ps
docker compose exec analytics-api curl --fail http://127.0.0.1:8090/healthz
~~~

Failure test: stop analytics-api, keep a stream running, confirm outbox files accumulate, restart the API, confirm replay/deletion, and verify duplicate delivery does not duplicate metrics.

## Implementation order

### Phase 1: foundation

Scaffold the Go module, configuration, PostgreSQL pool, Goose schema, health endpoints, Docker targets, and Compose services.

### Phase 2: ingestion

Implement event DTOs, validation, idempotent transactions, session updates, hourly upserts, and duplicate/retry/UTC-boundary tests.

### Phase 3: query API

Implement day/month/year ranges, coverage handling, summaries, live metrics, and tenant isolation.

### Phase 4: live producer

Refactor the file outbox, add the analytics client, add tracker deltas and interval peaks, and emit start/sample/end events.

### Phase 5: Laravel integration

Add the reader contract and remote client, update analytics/live-detail controllers, and add unavailable/null chart states.

### Phase 6: cutover

Deploy the service, compare local and remote results, record the UTC cutover, switch Laravel reads to remote, then stop local metric writes and compaction.

### Phase 7: cleanup

Remove unused Laravel analytics code only after verification. Retain legacy tables temporarily; dropping them requires a separately approved cleanup change.

## Definition of done

- Analytics has its own PostgreSQL database and volume.
- analytics-api scales independently from oxygen.
- golang-live continues streaming when analytics is unavailable.
- Events are durable, replayable, and idempotent.
- Day/month/year charts come from the analytics API.
- Pre-cutover data is unavailable rather than falsely zero.
- Laravel remains the only browser-facing authorization boundary.
- Dockerfile and Compose build/start all services successfully.
- Go race tests, Laravel contract tests, and Docker failure tests pass.
