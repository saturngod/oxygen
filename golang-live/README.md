# Oxygen Live Service

Go service for the live-streaming runtime. Laravel remains the control plane; this service owns runtime concerns:

- RTMP ingest for OBS publishers
- control endpoint for restart requests
- publish auth proxy into Laravel
- live session callbacks into Laravel
- HLS file serving
- viewer presence and durable analytics event reporting

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
| `FFMPEG_BIN` | `ffmpeg` | ffmpeg executable used for adaptive live transcoding |
| `FFMPEG_VIDEO_CODEC` | `libx264` | ffmpeg video encoder for live renditions |
| `LIVE_CALLBACK_ROOT` | `/tmp/oxygen-live/callbacks` | Persistent outbox for terminal Laravel callbacks |
| `ANALYTICS_URL` | empty | Private analytics API base URL; blank disables event delivery |
| `ANALYTICS_INGEST_TOKEN` | empty | Bearer token for analytics ingestion |
| `ANALYTICS_OUTBOX_ROOT` | `/tmp/oxygen-live/analytics-outbox` | Persistent outbox for analytics events |
| `ANALYTICS_BATCH_SIZE` | `100` | Maximum event count per durable analytics file |
| `LARAVEL_URL` | `http://127.0.0.1:8000` | Laravel app URL for internal callbacks |
| `LIVE_SERVICE_TOKEN` | empty | Shared token sent to Laravel `internal/live/*` routes |
| `LIVE_CONTROL_TOKEN` | empty | Bearer token required for Laravel control requests to this service |
| `LIVE_TRUST_PROXY_HEADERS` | `false` | Trust the first `X-Forwarded-For` address for viewer identity |
| `MAX_TRACKED_VIEWERS` | `100000` | Per-stream cap for in-memory current and unique viewer identities |
| `MAX_RTMP_CONNECTIONS` | `1000` | Maximum concurrent RTMP sockets, including handshakes |
| `MAX_LIVE_TRANSCODERS` | `2` | Maximum concurrent adaptive ffmpeg processes |
| `LIVE_HLS_STARTUP_TIMEOUT_SECONDS` | `30` | Total startup budget beginning with authenticated RTMP track discovery; delayed IDRs must arrive inside this window |
| `FFMPEG_RELAY_WRITE_TIMEOUT_SECONDS` | `10` | Maximum time a media write to ffmpeg may block |
| `FFMPEG_OUTPUT_STALL_TIMEOUT_SECONDS` | `10` | Maximum time any established adaptive rendition may stop advancing (not startup tolerance or playback latency) |
| `LIVE_FFMPEG_ANALYZE_DURATION_US` | `1000000` | Adaptive ffmpeg input analysis budget in microseconds |
| `LIVE_FFMPEG_PROBE_SIZE_BYTES` | `1048576` | Adaptive ffmpeg input probe budget in bytes |
| `VIEWER_TTL_SECONDS` | `45` | Viewer activity window |
| `ROLLUP_INTERVAL_SECONDS` | `15` | Snapshot flush interval |

## Production operation

- Mount `LIVE_CALLBACK_ROOT` on durable storage. Session-end callbacks are written there before stream cleanup and replayed until Laravel acknowledges them.
- Mount `ANALYTICS_OUTBOX_ROOT` when analytics delivery is enabled. Permanently rejected or corrupt entries are retained under each outbox's `dead-letter/` directory for operator inspection. Analytics storage is fail-open: an unavailable analytics outbox is logged but does not stop media service startup.
- Analytics admission stops at 64 MiB or 10,000 files (including dead letters and temporary files), or when a write would leave less than 256 MiB free. Existing entries are retained for replay; new telemetry can be lost while storage is constrained. Alert on analytics backlog/storage-pressure errors. Use a separate filesystem or quota for analytics to isolate media storage from other disk users; the free-space check is not a filesystem reservation.
- Put RTMP and HTTP behind appropriate network controls and TLS termination. Set `LIVE_TRUST_PROXY_HEADERS=true` only when the service is reachable exclusively through a proxy that replaces client-supplied forwarding headers.
- Use `GET /healthz` for liveness and `GET /readyz` for readiness. Startup fails unless the service tokens, ffmpeg binary, HLS root, and lifecycle callback outbox are usable; readiness then stays false until Laravel recovery succeeds and RTMP is listening.
- Drain the process with `SIGTERM`. The service stops accepting publishers, disconnects active RTMP connections, persists their terminal callbacks, and waits for handlers before exiting.
- Live streams created with a coding profile are transcoded into every selected rendition. Older streams without a profile continue to remux the source rendition.

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

For Harmonic Packager X, configure these GOP controls on the encoder feeding Packager X: progressive `1280x720` at `30000/1001`, a closed 60-frame GOP, and an IDR every GOP. Packager X forwards the RTMP publish, but Oxygen cannot force an IDR that the upstream source never sends. Confirm the actual IDR cadence on the RTMP input during commissioning.

On publish, the service validates `{public_id}` and `{stream_key}` with Laravel, discovers tracks, and registers an internal starting session. During startup, HLS requests return `503` with `Retry-After: 1`. Laravel is told that the session is live only after the master and every advertised media playlist reference a nonempty initialization file and enough completed segments. Adaptive streams require one completed segment per rendition; source-quality remuxing requires two.

The live output is stored as fMP4 HLS under `LIVE_HLS_ROOT` for each stream public id. Profile-based streams contain `index.m3u8` plus one `vN/playlist.m3u8` rendition playlist and `.m4s` segments per selected quality.

Startup tolerance, runtime stall detection, and player latency are separate settings. Increasing `LIVE_HLS_STARTUP_TIMEOUT_SECONDS` permits a late first IDR but does not make segments appear sooner. `FFMPEG_OUTPUT_STALL_TIMEOUT_SECONDS` is evaluated only after adaptive output is ready and is checked independently for every rendition.

## Soak verification

Run the real publisher-to-HLS test before a production release. Both adaptive and source-quality subtests publish RTMP and continuously decode HLS over HTTP. They reject playback stalls and check HLS disk usage and Go heap growth against 128 MiB limits, then verify terminal callback delivery. These checks do not measure ffmpeg RSS or replace capacity testing with production renditions and viewer load. The minimum duration is 15 seconds per mode.

```bash
OXYGEN_LIVE_SOAK=1 LIVE_SOAK_DURATION=36h \
  go test -run TestAdaptiveLiveStreamSoak -count=1 -timeout=73h ./internal/server
```

The two modes run sequentially, so this command takes approximately 72 hours. Use `/adaptive` or `/source` after the test name to select one mode. Validate reconnects, downstream outages, and production load on the target deployment before declaring 24/7 readiness.
