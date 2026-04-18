# golang-queue — Claude guide

This directory is a standalone Go worker that transcodes uploaded videos into HLS renditions. It is **not** part of the Laravel app's PHP runtime — it talks to the same Postgres database and Redis instance, but ships and deploys as its own binary.

Keep this file tight. If you need more detail on a decision, see `PLAN.md` (system design).

## What the worker does

1. Blocks on a Redis list (`BRPOP`) waiting for transcode jobs pushed by the Laravel app.
2. Loads the `media_files` row + its `media_file_profiles` row (qualities JSON) from Postgres.
3. Downloads the source from S3 to a temp working directory.
4. Runs `ffmpeg` once, emitting every rendition in the profile **plus** a master `main.m3u8` that references them.
5. Parses ffmpeg's `-progress pipe:1` stream and writes `media_files.progress` (0–100) + `status` back to Postgres every few seconds.
6. Uploads the HLS output tree back to S3, sets `media_files.streaming_url`, flips status to `success` (or `failed` on error), and cleans up the temp dir.

## Non-negotiables

- **Source of truth for renditions is `App\Enums\VideoQuality`** in the Laravel app (`app/Enums/VideoQuality.php`). The Go worker must keep a mirrored table of `value → {width, height, bitrate_kbps}` and must match it exactly. If the PHP enum changes, update the Go map in the same PR — do not diverge.
- **Status values must match `App\Enums\MediaFileStatus`**: `uploaded`, `progress`, `success`, `failed`. Do not invent new ones.
- **`media_files.progress` is `unsignedTinyInteger` (0–255 in PG, but semantically 0–100).** Never write >100. Clamp before updating.
- **Organization isolation.** Every query must filter by `organization_id` from the job payload. Do not trust the DB row alone — cross-check that the job's org matches the row's org before doing work.
- **The worker never mutates user-facing tables other than `media_files`** (status, progress, streaming_url, file_path if needed). Profiles, folders, organizations are read-only from Go.
- **ffmpeg is invoked as a subprocess.** Do not pull in cgo bindings. Shell out, stream stdout/stderr, kill on context cancel.
- **One ffmpeg invocation per job.** Use a single command with multiple `-map` outputs to produce all renditions + the master playlist in one pass. Do not loop per-bitrate — that re-decodes the source N times.
- **Idempotent-ish.** If a job is redelivered after a crash, the worker should be able to re-run without corrupting state: overwrite the HLS output prefix, re-set progress to 0, re-run ffmpeg. No partial-resume logic in v1.

## Job contract (Redis)

Jobs are pushed by Laravel to a Redis list. Exact key and payload shape are defined in `PLAN.md` under "Job contract" — treat that section as the wire protocol. If you change the shape, update both sides (Laravel dispatcher + Go consumer) in the same commit.

## Progress updates

- Parse ffmpeg `-progress pipe:1` key/value lines. Use `out_time_us` ÷ total duration (from `ffprobe` on the source before encoding starts).
- Throttle DB writes to at most once every ~2 seconds, or on whole-percent change, whichever is less frequent. Do not write on every progress line — that floods Postgres.
- Always write a final `progress = 100, status = success` row before releasing the job, and a `status = failed` (with progress left where it was) on error.

## Layout (target)

```
golang-queue/
  cmd/worker/main.go       # entrypoint, config, signal handling
  internal/config/         # env loading
  internal/queue/          # redis BRPOP consumer
  internal/db/             # pgx pool, media_files queries
  internal/s3/             # download source, upload HLS tree
  internal/transcode/      # ffmpeg command builder + progress parser
  internal/quality/        # mirror of VideoQuality enum
  PLAN.md                  # system design (read this before changing architecture)
  CLAUDE.md                # this file
  go.mod
```

Don't create this tree until the user asks for implementation — `PLAN.md` is the current deliverable.

## Commands (once implemented)

- `go run ./cmd/worker` — run one worker locally.
- `go build -o bin/worker ./cmd/worker` — build release binary.
- `go test ./...` — run tests. Every change that touches ffmpeg arg building, progress parsing, or the job contract must have a test.

## Do NOT

- Do not call into the Laravel HTTP API from Go. All coordination is via Postgres + Redis + S3.
- Do not use an ORM. Use `pgx` directly with explicit SQL.
- Do not cache DB rows across jobs. Re-read `media_files` at job start — the user may have deleted or renamed it.
- Do not write files outside the per-job temp dir. Clean it up in a deferred call, including on panic.
- Do not log the full ffmpeg stderr at INFO. It's enormous. Log at DEBUG and keep the last N lines for failure reports.
