# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# Oxygen — Video Transcoding & Live Streaming Platform

Laravel 13 + Inertia v3 + React 19 + Tailwind v4 web app plus **two independent Go services**:

- `golang-queue/` — VOD transcode worker. Consumes jobs from Redis, runs ffmpeg, writes HLS to S3.
- `golang-live/` — Live streaming service. Ingests RTMP, remuxes to live HLS (fMP4), tracks viewers. Controlled by Laravel via callbacks.

The Laravel app is the control plane (uploads, management, auth, session/viewer bookkeeping). Each Go service is its own Go module with its own `go.mod`, `.env.example`, and `CLAUDE.md`.

## Dev Environment

```bash
composer run setup          # first-time: install, migrate, build
composer run dev            # concurrently runs: php serve, queue:listen, pail, vite dev
```

If frontend changes aren't visible, the user likely needs `npm run build` or `composer run dev`.

## Verification Pipeline

After changes, run the relevant steps:

```bash
vendor/bin/pint --dirty --format agent   # PHP formatting (always run after PHP edits)
npm run lint:check                        # ESLint (generated dirs are already ignored)
npm run format:check                      # Prettier on resources/
npm run types:check                       # TypeScript tsc --noEmit
php artisan test --compact                # Pest tests (SQLite in-memory, sync queue)
php artisan test --compact --filter=name  # single test
```

Full CI equivalent: `composer ci:check` (lint + format + types + tests).

## Generated Code — Do Not Hand-Edit

These directories are auto-generated and overwritten:

- `resources/js/actions/` — Wayfinder controller action functions
- `resources/js/routes/` — Wayfinder named-route functions
- `resources/js/wayfinder/` — Wayfinder internals
- `resources/js/components/ui/` — shadcn/ui components (use `npx shadcn` to update)

After adding or changing Laravel routes, run `php artisan wayfinder:generate` (or just `npm run dev` which triggers it via Vite).

Import Wayfinder functions from `@/actions/` (controllers) or `@/routes/` (named routes) — never hardcode URLs.

## Key Enums (Single Source of Truth)

- `App\Enums\VideoQuality` — 7 cases (Sd240p–Uhd2160p) with width/height/bitrate. **The frontend, the Go transcode worker, AND the Go live service must all mirror this enum.** If you change it, update all four in the same PR (PHP enum, `resources/js`, `golang-queue/internal/quality`, `golang-live`).
- `App\Enums\MediaFileStatus` — `uploaded`, `progress`, `success`, `failed`.
- `App\Enums\OrganizationRole` — `admin`, `operator`.
- `App\Enums\LiveStreamStatus` — `idle`, `live`, `offline`, `restarting`, `failed`, `disabled` (has `label()`).
- `App\Enums\LiveStreamSessionStatus` — `starting`, `live`, `ended`, `failed`.
- `App\Enums\WebhookEvent` — `file_uploaded`, `file_status_changed` (has `label()`).

## Models

`User`, `Organization` (belongsToMany via `organization_user` pivot with `role`), `Folder`, `MediaFile`, `MediaFileProfile`, `Profile`, `Webhook`.

Live streaming: `LiveStream` (owns stream, encrypted `stream_key`, status, restart flags), `LiveStreamSession` (one broadcast session: status, hls_url, viewer/segment metrics), `LiveStreamViewerRollup` (minute-level viewer snapshots).

## Organizations & Multi-Tenancy

Multi-tenant at the organization level. A user may belong to many orgs; the pivot carries `role` (`admin`|`operator`). At registration, a new org is created and the user attached as `admin`.

### Active organization (session-scoped)

Resolved in `HandleInertiaRequests::share()`:

1. Read `current_organization_id` from session.
2. Find that org in the user's memberships; fall back to first membership (alphabetical).
3. Rewrite session if stale.

Shared Inertia props: `auth.user.current_role`, `auth.user.current_organization`, `auth.organizations`.

When adding server-side code needing the active org, use `session('current_organization_id')` — do NOT re-derive from "first membership."

### Route permission layering (`routes/web.php`)

Three concentric groups — pick the right one, do not duplicate checks in controllers:

1. **`auth` + `verified`** — only `PUT organizations/{organization}/switch` sits here directly.
2. **`EnsureOrganizationMember`** (nested in 1) — normal app surface: dashboard, manage/*, status.
3. **`EnsureOrganizationAdmin`** (nested in 1) — `admin/organizations/{organization}/*`: settings, users, profiles, webhooks, **live-streams** (list/create/show/update + `rotate-key`/`restart`/`disable`).

Plus one **unauthenticated** group: `/internal/live/*` — service-to-service callbacks from `golang-live` (`auth-publish`, `session-started`, `session-ended`, `session-failed`, `recover-active`, `viewer-snapshot`), handled by `LiveStreamServiceController`. These are authenticated by a shared service token, not session auth — never add `auth` middleware here.

### Registration gate

Toggle via `ALLOW_REGISTER` in `.env`. Evaluated in `config/fortify.php`, conditionally includes `Features::registration()`. UI auto-hides via `canRegister` prop.

## Media Uploads (S3 Direct Multipart)

Browser uploads directly to S3 via presigned multipart. Server never proxies bytes.

Flow (all under `manage/files/multipart/*`):

1. `init` — validates folder ownership, generates S3 key, caches upload context under `uploadId`.
2. `sign-part` — presigned UploadPart URL. Browser PUTs chunk to S3.
3. `complete` — finalizes upload, reads size via headObject, creates `MediaFile` row.
4. `abort` — cancels upload, evicts cache entry.

**Security**: Never trust `uploadId`, `key`, or `folder_id` from the client. Always re-read from cached upload context and re-check `organization_id` against session.

Other endpoints: `POST manage/files/url` (server-side URL ingest), `DELETE manage/files/{mediaFile}` (refuses if status is `Progress`).

## Coding Profiles

Each org has many `Profile` rows (uuid, `organization_id`, `name`, `qualities` json, `is_default`).

- Quality catalog: `App\Enums\VideoQuality` — edit the enum, not the frontend. Validation uses `Rule::enum(VideoQuality::class)`.
- Exactly one default per org. `makeDefault` runs in a `DB::transaction` clearing the previous default before promoting.
- Admin-only routes under `admin/organizations/{organization}/profiles*`.
- Frontend: `resources/js/pages/admin/profiles/` — use design tokens, not hardcoded colors.

## Live Streaming (Laravel + `golang-live/`)

OBS/publisher → `golang-live` (RTMP ingest) → live HLS (fMP4) playback. Laravel is the control plane; `golang-live` does the media work.

Flow:

1. Admin creates a `LiveStream` (gets RTMP URL + encrypted stream key) under `admin/organizations/{org}/live-streams`.
2. Publisher pushes RTMP to `golang-live`, which calls Laravel `POST /internal/live/auth-publish` to validate the key and resolve the org/stream.
3. `golang-live` reports lifecycle via `session-started` / `session-ended` / `session-failed`, creating/updating `LiveStreamSession` rows.
4. Viewer presence is sampled and flushed to `viewer-snapshot`, persisted as `LiveStreamViewerRollup` (minute-level).

Laravel side: `OrganizationLiveStreamsController` (admin CRUD), `LiveStreamServiceController` (internal callbacks), `LiveStreamControlClient` + `LiveStreamEndpointService` (talk to `golang-live`). Frontend: `resources/js/pages/admin/live-streams/`.

**Security**: `/internal/live/*` trusts only the shared service token + the stream key it validates; always re-check `organization_id` on session/viewer writes.

## Go Transcode Worker (`golang-queue/`)

Standalone Go binary (not part of PHP runtime). Talks to shared Postgres + Redis + S3.

- Entry: `cmd/worker/main.go`; `WORKER_CONCURRENCY` goroutines (default 1).
- Consumes from Redis list `QUEUE_KEY` (default `oxygen-database-queues:transcode`) via BRPOP.
- Reads profile from Postgres (does not trust job payload for qualities).
- Cross-checks `organization_id` for isolation.
- Single ffmpeg invocation per job (`-filter_complex` + `split`, all renditions + master playlist — never loop per-bitrate).
- Writes progress to `media_files` table; uploads HLS tree to S3 (separate source vs. streaming buckets via `SOURCE_AWS_*` / `STREAMING_AWS_*`).
- Pushes webhook events to `{QUEUE_KEY}:webhooks` for Laravel to deliver (3 attempts, backoff `[5s, 30s, 120s]`).
- Run: `go run ./cmd/worker` | Build: `go build -o bin/worker ./cmd/worker` | Test: `go test ./...`

See `golang-queue/PLAN.md` for full system design, `golang-queue/CLAUDE.md` for Go-specific conventions. `golang-live/` has its own `README.md` and uses `gohlslib/v2` (HLS muxing) + `gortmplib` (RTMP ingest).

## Frontend Conventions

- Path alias: `@/*` → `./resources/js/*`
- UI components: shadcn/ui (Radix-based) + Lucide icons + Headless UI
- React Compiler enabled via `babel-plugin-react-compiler`
- Inertia v3: use `Inertia::optional()` (not `lazy()`), `router.cancelAll()` (not `cancel()`)
- Deferred props: add skeleton/empty state with pulsing animation
- Use `<Link>`, `<Form>`, `useForm` from `@inertiajs/react`

## Testing

- Pest 4. Create with `php artisan make:test --pest {name}`.
- In-memory SQLite, sync queue driver, array cache/session.
- Use model factories. Check for custom factory states before manual setup.
