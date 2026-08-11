# Oxygen analytics service

This standalone Go service owns viewer analytics ingestion and reads. It uses
its own PostgreSQL database and never joins the Laravel database. `golang-live`
writes signed event batches to a durable disk outbox and retries them here.

```bash
cp .env.example .env
go run ./cmd/analytics serve  # migrates once on startup, then serves HTTP
```

The standalone `migrate` command remains available for an operator who wants
to apply schema changes without starting the API. Normal Compose startup does
not need a separate migration container.

Private endpoints:

- `POST /internal/v1/events/batch` with the ingest bearer token
- `GET /internal/v1/organizations/{organizationID}/streams/{streamID}/analytics?period=day|month|year`
- `GET /internal/v1/organizations/{organizationID}/streams/{streamID}/live`
- `GET /healthz` and `/readyz`

Raw events are retained for the configured period; hourly and session metrics
remain available after raw-event pruning. The service reports UTC coverage and
uses `null` points before the first accepted event rather than presenting them
as zero activity.
