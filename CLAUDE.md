# Oxygen — Video Transcoding Platform

Laravel 13 + Inertia v3 + React 19 + Tailwind v4 app with a separate Go transcoding worker (`golang-queue/`). The Laravel app handles uploads and management; the Go worker consumes jobs from Redis and runs ffmpeg.

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

- `App\Enums\VideoQuality` — 7 cases (Sd240p–Uhd2160p) with width/height/bitrate. **Both the frontend and the Go worker must mirror this enum.** If you change it, update all three in the same PR.
- `App\Enums\MediaFileStatus` — `uploaded`, `progress`, `success`, `failed`.
- `App\Enums\OrganizationRole` — `admin`, `operator`.

## Models

`User`, `Organization` (belongsToMany via `organization_user` pivot with `role`), `Folder`, `MediaFile`, `MediaFileProfile`, `Profile`.

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
3. **`EnsureOrganizationAdmin`** (nested in 1) — `admin/organizations/{organization}/*`: settings, users, profiles.

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

## Go Worker (`golang-queue/`)

Standalone Go binary (not part of PHP runtime). Talks to shared Postgres + Redis + S3.

- Entry: `cmd/worker/main.go`
- Consumes from Redis list `queues:transcode` (BRPOP)
- Reads profile from Postgres (does not trust job payload for qualities)
- Cross-checks `organization_id` for isolation
- Single ffmpeg invocation per job (all renditions + master playlist)
- Writes progress to `media_files` table; uploads HLS tree to S3
- Run: `go run ./cmd/worker` | Build: `go build -o bin/worker ./cmd/worker` | Test: `go test ./...`

See `golang-queue/PLAN.md` for full system design, `golang-queue/CLAUDE.md` for Go-specific conventions.

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
