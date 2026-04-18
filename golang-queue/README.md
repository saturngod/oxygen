# golang-queue

Standalone Go worker that consumes video transcode jobs from Redis, transcodes them into HLS (multi-bitrate `main.m3u8`), uploads the output to S3, and writes progress back to the shared Postgres database.

## Architecture

```
Laravel  --LPUSH-->  Redis  --BRPOP-->  Go Worker
                                        │
                       ┌────────────────┼────────────────┐
                       │                │                 │
                       ▼                ▼                 ▼
                  Postgres          S3 (source)       ffmpeg
                  read profile      presigned URL      HLS transcode
                  write progress                       │
                                                       ▼
                                                  S3 (streaming)
                                                  upload HLS tree
```

### Per-job pipeline

1. **BRPOP** job from `queues:transcode`
2. **Load** `media_files` + `media_file_profiles` from Postgres (validate org ownership)
3. **Presign** source S3 URL → pass directly as ffmpeg input (no local download)
4. **Transcode** via ffmpeg — single invocation with all renditions from the profile's `qualities` array
5. **Upload** HLS output tree (`.m3u8` + `.ts` segments) to streaming bucket
6. **Update** `media_files.status = success`, `streaming_url`, `progress = 100`

## Requirements

- Go 1.25+
- ffmpeg / ffprobe installed and in `$PATH`
- Shared Redis, Postgres, and S3 (MinIO compatible) with the Laravel app

## Quick start

```bash
cp .env.example .env
# Edit .env with your Redis, Postgres, and S3 credentials

go run ./cmd/worker
```

For a release build:

```bash
go build -o bin/worker ./cmd/worker
./bin/worker
```

## Configuration

All config is via environment variables (or `.env` file in development).

### Redis

| Variable         | Default                            | Description                                  |
| ---------------- | ---------------------------------- | -------------------------------------------- |
| `REDIS_ADDR`     | `127.0.0.1:6379`                   | Redis host:port                              |
| `REDIS_PASSWORD` | _(empty)_                          | Redis password                               |
| `REDIS_DB`       | `0`                                | Redis database number                        |
| `QUEUE_KEY`      | `oxygen-database-queues:transcode` | Redis list key (must include Laravel prefix) |

### Database (Postgres)

| Variable       | Default               | Description                                           |
| -------------- | --------------------- | ----------------------------------------------------- |
| `DATABASE_URL` | _(built from DB_\*)\_ | Full `postgres://` DSN. If set, overrides DB\_\* vars |
| `DB_HOST`      | `127.0.0.1`           |                                                       |
| `DB_PORT`      | `5432`                |                                                       |
| `DB_DATABASE`  | _(empty)_             |                                                       |
| `DB_USERNAME`  | `postgres`            |                                                       |
| `DB_PASSWORD`  | _(empty)_             |                                                       |

### S3 — Source bucket (download originals)

| Variable                             | Default        | Description                                 |
| ------------------------------------ | -------------- | ------------------------------------------- |
| `SOURCE_AWS_BUCKET`                  | `AWS_BUCKET`   | Bucket where Laravel stores uploaded videos |
| `SOURCE_AWS_ENDPOINT`                | `AWS_ENDPOINT` | Custom endpoint (MinIO, localstack)         |
| `SOURCE_AWS_URL`                     | `AWS_URL`      | URL prefix for generating presigned URLs    |
| `SOURCE_AWS_USE_PATH_STYLE_ENDPOINT` | `false`        | Use path-style S3 URLs                      |

### S3 — Streaming bucket (upload HLS output)

Can be the same bucket as source, or a separate CDN-fronted bucket.

| Variable                                | Default              | Description                                             |
| --------------------------------------- | -------------------- | ------------------------------------------------------- |
| `STREAMING_AWS_BUCKET`                  | `AWS_BUCKET`         | Bucket for HLS output                                   |
| `STREAMING_AWS_ENDPOINT`                | `AWS_ENDPOINT`       | Custom endpoint                                         |
| `STREAMING_AWS_URL`                     | `AWS_URL`            | URL prefix — used to build `streaming_url` stored in DB |
| `STREAMING_AWS_USE_PATH_STYLE_ENDPOINT` | `false`              | Use path-style S3 URLs                                  |
| `STREAMING_AWS_DEFAULT_REGION`          | `AWS_DEFAULT_REGION` | Region for streaming bucket                             |

### Shared AWS credentials

| Variable                | Default     | Description                |
| ----------------------- | ----------- | -------------------------- |
| `AWS_ACCESS_KEY_ID`     | _(empty)_   | Shared across both buckets |
| `AWS_SECRET_ACCESS_KEY` | _(empty)_   | Shared across both buckets |
| `AWS_DEFAULT_REGION`    | `us-east-1` | Default region             |

### Transcode

| Variable                   | Default           | Description                                                  |
| -------------------------- | ----------------- | ------------------------------------------------------------ |
| `HLS_PREFIX`               | `hls`             | S3 key prefix for HLS output                                 |
| `WORK_DIR`                 | `/tmp/transcoder` | Local scratch space (cleaned up after each job)              |
| `FFMPEG_BIN`               | `ffmpeg`          | Path to ffmpeg binary                                        |
| `FFPROBE_BIN`              | `ffprobe`         | Path to ffprobe binary                                       |
| `FFMPEG_VIDEO_CODEC`       | `libx264`         | Video encoder (`libx264`, `h264_videotoolbox`, `h264_nvenc`) |
| `PROGRESS_MIN_INTERVAL_MS` | `2000`            | Minimum interval between DB progress writes                  |
| `WORKER_CONCURRENCY`       | `1`               | Number of parallel ffmpeg jobs                               |

## S3 path layout

**Source** (uploaded by Laravel):

```
s3://source-bucket/media/{org_id}/{uuid}.mp4
```

**HLS output** (uploaded by worker):

```
s3://streaming-bucket/hls/{org_id}/{media_file_id}/main.m3u8
s3://streaming-bucket/hls/{org_id}/{media_file_id}/v0/playlist.m3u8
s3://streaming-bucket/hls/{org_id}/{media_file_id}/v0/segment_000.ts
s3://streaming-bucket/hls/{org_id}/{media_file_id}/v1/playlist.m3u8
...
```

**`streaming_url` stored in DB:**

```
{STREAMING_AWS_URL}/hls/{org_id}/{media_file_id}/main.m3u8
```

## Project structure

```
golang-queue/
  cmd/worker/main.go              # Entrypoint, signal handling, wiring
  internal/
    config/config.go              # Env loading
    db/store.go                   # pgx pool, media_files queries
    s3/client.go                  # Source presign + streaming upload
    quality/quality.go            # Mirror of PHP VideoQuality enum
    queue/consumer.go             # Redis BRPOP consumer + full pipeline
    transcode/transcoder.go       # ffmpeg command builder + progress parser
  .env.example
  PLAN.md                         # System design doc
  CLAUDE.md                       # AI dev guide
```

## Supported qualities

Must match `App\Enums\VideoQuality` in the Laravel app exactly.

| Quality | Resolution  | Video bitrate | Audio bitrate |
| ------- | ----------- | ------------- | ------------- |
| 240p    | 352 x 240   | 600 kbps      | 64 kbps       |
| 360p    | 640 x 360   | 800 kbps      | 96 kbps       |
| 480p    | 842 x 480   | 1,400 kbps    | 128 kbps      |
| 720p    | 1280 x 720  | 2,800 kbps    | 128 kbps      |
| 1080p   | 1920 x 1080 | 5,000 kbps    | 192 kbps      |
| 1440p   | 2560 x 1440 | 8,000 kbps    | 192 kbps      |
| 2160p   | 3840 x 2160 | 25,000 kbps   | 192 kbps      |

## Job contract

Laravel pushes a JSON payload via `Redis::lpush('queues:transcode', ...)`:

```json
{
    "id": "uuid",
    "organization_id": "uuid",
    "folder_id": "uuid|null",
    "title": "My Video",
    "file_name": "video.mp4",
    "file_path": "media/{org_id}/{uuid}.mp4",
    "source_url": null,
    "streaming_url": null,
    "size": 4692397,
    "status": "uploaded",
    "progress": 0,
    "created_at": "2026-04-16T13:00:00Z",
    "updated_at": "2026-04-16T13:00:00Z"
}
```

The worker re-reads the profile from Postgres (does not trust the payload for qualities) and cross-checks `organization_id` for isolation.

## Testing

```bash
go test ./...
```

## Graceful shutdown

Send `SIGINT` or `SIGTERM`. The worker stops accepting new jobs and waits for in-flight ffmpeg processes to finish.
