# Oxygen Live Service

Go service for the live-streaming runtime. Laravel remains the control plane; this service owns runtime concerns:

- RTMP ingest for OBS publishers
- control endpoint for restart requests
- publish auth proxy into Laravel
- live session callbacks into Laravel
- HLS file serving
- viewer presence and minute snapshot reporting

This module intentionally does not share code with `golang-queue`: the queue worker is batch VOD work, while this service is long-running live network work.

## Run

```bash
go run ./cmd/live
```

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `LIVE_ADDR` | `:8081` | HTTP listen address |
| `LIVE_RTMP_ADDR` | `:1935` | RTMP ingest listen address |
| `LIVE_HLS_ROOT` | `/tmp/oxygen-live/hls` | Local HLS root, one directory per stream public id |
| `LARAVEL_URL` | `http://127.0.0.1:8000` | Laravel app URL for internal callbacks |
| `LIVE_SERVICE_TOKEN` | empty | Shared token sent to Laravel `internal/live/*` routes |
| `LIVE_CONTROL_TOKEN` | empty | Bearer token required for Laravel control requests to this service |
| `VIEWER_TTL_SECONDS` | `45` | Viewer activity window |
| `ROLLUP_INTERVAL_SECONDS` | `15` | Snapshot flush interval |

## OBS Publishing

The service listens for RTMP publishes on `LIVE_RTMP_ADDR`. OBS should use:

```text
Server:     rtmp://127.0.0.1:1935/live
Stream key: {public_id}?key={stream_key}
```

Recommended OBS settings:

```text
Keyframe interval: 2 seconds
Rate control:      CBR
B-frames:          0 if available
```

On publish, the service validates `{public_id}` and `{stream_key}` with Laravel, starts a live session, remuxes RTMP media into HLS, and exposes playback through `GET /live/{public_id}/index.m3u8`.

The live output is stored as fMP4 HLS under `LIVE_HLS_ROOT` for each stream public id. You will see playlists plus `.mp4` media files such as `*_init.mp4`, `*_segNN.mp4`, and `*_partNNN.mp4`. This service does not write `.ts` segments for live playback.
