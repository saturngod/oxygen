# golang-queue — Agent Guide

Standalone Go worker that consumes video transcode jobs from Redis, transcodes via ffmpeg into multi-bitrate HLS, uploads to S3, and writes progress to the shared Postgres database. Ships as its own binary — not part of the Laravel PHP runtime.

For system design, see `PLAN.md`. For the Laravel app's media upload flow and S3 multipart endpoints, see the parent `AGENTS.md`.

## Commands

```
go run ./cmd/worker          # run locally (requires .env)
go build -o bin/worker ./cmd/worker   # release binary
go test ./...                # run tests (none exist yet)
```

No lint/typecheck config. No Makefile or task runner.

## Module & Go version

- **Module:** `oxygen/worker` (import path is `oxygen/worker/internal/...`)
- **Go 1.25.1**
- Dependencies: `pgx/v5`, `go-redis/v9`, `aws-sdk-go-v2`, `godotenv`

## Architecture (one-line summary per package)

| Package | Purpose |
|---|---|
| `cmd/worker` | Entrypoint, config load, signal handling, goroutine pool |
| `internal/config` | Env loading via godotenv (`.env` in dev, real env in prod) |
| `internal/queue` | Redis BRPOP consumer, full job pipeline orchestration |
| `internal/db` | pgx pool, `media_files` + `media_file_profiles` queries |
| `internal/s3` | Source download + streaming bucket upload (two separate S3 clients) |
| `internal/transcode` | ffmpeg command builder, progress parser, codec auto-detect |
| `internal/quality` | Mirror of Laravel's `VideoQuality` enum |

## Non-negotiable rules

- **Quality map** (`internal/quality/quality.go`) must stay in lockstep with `App\Enums\VideoQuality` in the Laravel app (`app/Enums/VideoQuality.php`). Same string keys, same width/height/bitrate. Update both sides in the same PR.
- **Status values** must match `App\Enums\MediaFileStatus`: `uploaded`, `progress`, `success`, `failed`. Never invent new ones.
- **Progress is clamped 0–100.** The DB column is `unsignedTinyInteger` (0–255) but semantically 0–100.
- **Organization isolation.** Every query filters by `organization_id` from the job payload. Cross-check before doing work.
- **Only `media_files` is writable** (status, progress, streaming_url, updated_at). Profiles, folders, organizations are read-only.
- **One ffmpeg invocation per job.** Single command with `-filter_complex` + `split`, multiple `-map` outputs. Never loop per-bitrate.
- **ffmpeg as subprocess only.** No cgo bindings. Kill on context cancel.
- **No ORM.** Use `pgx` with explicit SQL.
- **No Laravel HTTP calls.** Coordination is via Postgres + Redis + S3 only.

## Job payload (actual, not PLAN.md)

PLAN.md describes a minimal payload with `job_id`, `media_file_id`, etc. The **actual** payload pushed by Laravel is the full `media_files` row:

```json
{
  "id": "uuid",
  "organization_id": "uuid",
  "folder_id": "uuid|null",
  "title": "My Video",
  "file_name": "video.mp4",
  "file_path": "media/{org_id}/{uuid}.mp4",
  "source_url": null,
  "size": 4692397,
  "status": "uploaded",
  "progress": 0,
  "created_at": "...",
  "updated_at": "..."
}
```

The worker re-reads `media_file_profiles` from Postgres (does not trust payload for qualities). If you change the payload shape, update both Laravel dispatcher and Go consumer in the same commit.

## Config gotchas

- **QUEUE_KEY** default is `oxygen-database-queues:transcode` — includes Laravel's Redis prefix. Must match what Laravel uses.
- **FFMPEG_VIDEO_CODEC** default is `auto` (runtime detection in `internal/transcode/detect.go`): tries VideoToolbox on macOS, then NVENC, then falls back to `libx264`.
- **Separate source/streaming S3 buckets.** `SOURCE_AWS_*` and `STREAMING_AWS_*` vars override shared `AWS_*` defaults. The streaming bucket's `STREAMING_AWS_URL` is used to build `streaming_url` stored in DB.
- `DATABASE_URL` overrides individual `DB_*` vars. Fallback builds a DSN from `DB_HOST`/`DB_PORT`/etc.
- `REDIS_PASSWORD` normalizes the literal string `"null"` to empty (Laravel convention).

## Pipeline flow (per job)

1. BRPOP from queue key (30s timeout, graceful on context cancel)
2. Decode JSON payload, validate `id` + `organization_id` present
3. Load `media_files` row from Postgres (org-scoped query)
4. Load `media_file_profiles` row → get `qualities` array
5. Set `status=progress, progress=0`
6. Create per-job temp dir under `WORK_DIR` (deferred cleanup)
7. Download source from S3 to local file, OR use `source_url` if no `file_path`
8. Probe duration with ffprobe
9. Build ffmpeg command from quality map, run with `-progress pipe:1`
10. Parse `out_time_us` lines → compute percent, throttle DB writes (≥2s interval + whole-percent change)
11. Upload HLS tree to streaming bucket
12. Set `status=success, progress=100, streaming_url=<url>`
13. On any error: `status=failed`, progress left as-is

## Things an agent might get wrong

- **ffmpeg stderr is captured in a 200-line ring buffer**, not logged at INFO. Only emitted on failure. Do not change this — ffmpeg stderr is enormous.
- **Progress is capped at 99 during encoding** and only set to 100 after ffmpeg exits cleanly.
- **Temp dir cleanup is deferred**, including on panic. Do not write files outside the per-job temp dir.
- **No DB row caching across jobs.** Re-read `media_files` at job start.
- **`filter_complex` uses `split=N`** to avoid re-decoding the source. The builder is in `transcoder.go` — command is assembled programmatically from the profile's qualities array.
