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

## Laravel Octane (FrankenPHP)

The app runs under Octane with the **FrankenPHP** driver (`OCTANE_SERVER=frankenphp`). Octane boots the container once and reuses it across requests, so the cardinal rule is **no request-derived state in long-lived bindings**.

```bash
php artisan octane:start --max-requests=500   # serve (recycles workers every 500 req)
php artisan octane:reload                      # after deploy: load new code into workers
php artisan octane:start --watch               # dev: auto-reload on file change
```

Keep it Octane-safe when adding code:

- Do NOT bind anything derived from `$request`, `Auth::user()`, or `session()` into a `singleton` or static property. Resolve per request (use the `request()`/`auth()`/`session()` helpers inside methods). The active org already follows this — `HandleInertiaRequests::share()` reads `session('current_organization_id')` every request; never cache it in a singleton.
- If you must keep stateful state, add the service to the `flush` list in `config/octane.php` or reset it on `RequestTerminated`.
- No static arrays that grow per request (memory leak). Register event listeners in providers, never in request handlers.
- Don't read superglobals (`$_GET`/`$_POST`/`$_SERVER`) — use the `Request` object.
- `Octane::concurrently()`, `Octane::table()`, the `octane` cache, and ticks are **Swoole-only** — do not use them on FrankenPHP.
- The Go workers (`golang-queue`, `golang-live`) and `queue:listen` are separate processes — Octane only replaces the HTTP server.

## Testing

- Pest 4. Create with `php artisan make:test --pest {name}`.
- In-memory SQLite, sync queue driver, array cache/session.
- Use model factories. Check for custom factory states before manual setup.

===

<laravel-boost-guidelines>
=== foundation rules ===

# Laravel Boost Guidelines

The Laravel Boost guidelines are specifically curated by Laravel maintainers for this application. These guidelines should be followed closely to ensure the best experience when building Laravel applications.

## Foundational Context

This application is a Laravel application and its main Laravel ecosystems package & versions are below. You are an expert with them all. Ensure you abide by these specific packages & versions.

- php - 8.4
- inertiajs/inertia-laravel (INERTIA_LARAVEL) - v3
- laravel/fortify (FORTIFY) - v1
- laravel/framework (LARAVEL) - v13
- laravel/octane (OCTANE) - v2
- laravel/prompts (PROMPTS) - v0
- laravel/wayfinder (WAYFINDER) - v0
- laravel/boost (BOOST) - v2
- laravel/mcp (MCP) - v0
- laravel/pail (PAIL) - v1
- laravel/pint (PINT) - v1
- laravel/sail (SAIL) - v1
- pestphp/pest (PEST) - v4
- phpunit/phpunit (PHPUNIT) - v12
- @inertiajs/react (INERTIA_REACT) - v3
- react (REACT) - v19
- tailwindcss (TAILWINDCSS) - v4
- @laravel/vite-plugin-wayfinder (WAYFINDER_VITE) - v0
- eslint (ESLINT) - v9
- prettier (PRETTIER) - v3

## Skills Activation

This project has domain-specific skills available. You MUST activate the relevant skill whenever you work in that domain—don't wait until you're stuck.

- `fortify-development` — ACTIVATE when the user works on authentication in Laravel. This includes login, registration, password reset, email verification, two-factor authentication (2FA/TOTP/QR codes/recovery codes), profile updates, password confirmation, or any auth-related routes and controllers. Activate when the user mentions Fortify, auth, authentication, login, register, signup, forgot password, verify email, 2FA, or references app/Actions/Fortify/, CreateNewUser, UpdateUserProfileInformation, FortifyServiceProvider, config/fortify.php, or auth guards. Fortify is the frontend-agnostic authentication backend for Laravel that registers all auth routes and controllers. Also activate when building SPA or headless authentication, customizing login redirects, overriding response contracts like LoginResponse, or configuring login throttling. Do NOT activate for Laravel Passport (OAuth2 API tokens), Socialite (OAuth social login), or non-auth Laravel features.
- `laravel-best-practices` — Apply this skill whenever writing, reviewing, or refactoring Laravel PHP code. This includes creating or modifying controllers, models, migrations, form requests, policies, jobs, scheduled commands, service classes, and Eloquent queries. Triggers for N+1 and query performance issues, caching strategies, authorization and security patterns, validation, error handling, queue and job configuration, route definitions, and architectural decisions. Also use for Laravel code reviews and refactoring existing Laravel code to follow best practices. Covers any task involving Laravel backend PHP code patterns.
- `octane-development` — Use this skill when working with Laravel Octane, a long-running PHP worker server (Swoole, FrankenPHP, RoadRunner) where the application boots once and serves many requests instead of rebooting for each request like PHP-FPM. Trigger when installing Octane or starting its server; configuring or detecting the active driver for driver-specific code; using Octane::concurrently(), Octane::table(), the Octane cache, or shared in-memory state across workers; controlling worker memory growth; or testing Octane behavior. Skip plain PHP-FPM applications with no persistent worker.
- `wayfinder-development` — Use this skill for Laravel Wayfinder which auto-generates typed functions for Laravel controllers and routes. ALWAYS use this skill when frontend code needs to call backend routes or controller actions. Trigger when: connecting any React/Vue/Svelte/Inertia frontend to Laravel controllers, routes, building end-to-end features with both frontend and backend, wiring up forms or links to backend endpoints, fixing route-related TypeScript errors, importing from @/actions or @/routes, or running wayfinder:generate. Use Wayfinder route functions instead of hardcoded URLs. Covers: wayfinder() vite plugin, .url()/.get()/.post()/.form(), query params, route model binding, tree-shaking. Do not use for backend-only task
- `pest-testing` — Use this skill for Pest PHP testing in Laravel projects only. Trigger whenever any test is being written, edited, fixed, or refactored — including fixing tests that broke after a code change, adding assertions, converting PHPUnit to Pest, adding datasets, and TDD workflows. Always activate when the user asks how to write something in Pest, mentions test files or directories (tests/Feature, tests/Unit, tests/Browser), or needs browser testing, smoke testing multiple pages for JS errors, or architecture tests. Covers: test()/it()/expect() syntax, datasets, mocking, browser testing (visit/click/fill), smoke testing, arch(), Livewire component tests, RefreshDatabase, and all Pest 4 features. Do not use for factories, seeders, migrations, controllers, models, or non-test PHP code.
- `inertia-react-development` — Develops Inertia.js v3 React client-side applications. Activates when creating React pages, forms, or navigation; using <Link>, <Form>, useForm, useHttp, setLayoutProps, or router; working with deferred props, prefetching, optimistic updates, instant visits, or polling; or when user mentions React with Inertia, React pages, React forms, or React navigation.
- `tailwindcss-development` — Always invoke when the user's message includes 'tailwind' in any form. Also invoke for: building responsive grid layouts (multi-column card grids, product grids), flex/grid page structures (dashboards with sidebars, fixed topbars, mobile-toggle navs), styling UI components (cards, tables, navbars, pricing sections, forms, inputs, badges), adding dark mode variants, fixing spacing or typography, and Tailwind v3/v4 work. The core use case: writing or fixing Tailwind utility classes in HTML templates (Blade, JSX, Vue). Skip for backend PHP logic, database queries, API routes, JavaScript with no HTML/CSS component, CSS file audits, build tool configuration, and vanilla CSS.

## Conventions

- You must follow all existing code conventions used in this application. When creating or editing a file, check sibling files for the correct structure, approach, and naming.
- Use descriptive names for variables and methods. For example, `isRegisteredForDiscounts`, not `discount()`.
- Check for existing components to reuse before writing a new one.

## Verification Scripts

- Do not create verification scripts or tinker when tests cover that functionality and prove they work. Unit and feature tests are more important.

## Application Structure & Architecture

- Stick to existing directory structure; don't create new base folders without approval.
- Do not change the application's dependencies without approval.

## Frontend Bundling

- If the user doesn't see a frontend change reflected in the UI, it could mean they need to run `npm run build`, `npm run dev`, or `composer run dev`. Ask them.

## Documentation Files

- You must only create documentation files if explicitly requested by the user.

## Replies

- Be concise in your explanations - focus on what's important rather than explaining obvious details.

=== boost rules ===

# Laravel Boost

## Tools

- Laravel Boost is an MCP server with tools designed specifically for this application. Prefer Boost tools over manual alternatives like shell commands or file reads.
- Use `database-query` to run read-only queries against the database instead of writing raw SQL in tinker.
- Use `database-schema` to inspect table structure before writing migrations or models.
- Use `get-absolute-url` to resolve the correct scheme, domain, and port for project URLs. Always use this before sharing a URL with the user.
- Use `browser-logs` to read browser logs, errors, and exceptions. Only recent logs are useful, ignore old entries.

## Searching Documentation (IMPORTANT)

- Always use `search-docs` before making code changes. Do not skip this step. It returns version-specific docs based on installed packages automatically.
- Pass a `packages` array to scope results when you know which packages are relevant.
- Use multiple broad, topic-based queries: `['rate limiting', 'routing rate limiting', 'routing']`. Expect the most relevant results first.
- Do not add package names to queries because package info is already shared. Use `test resource table`, not `filament 4 test resource table`.

### Search Syntax

1. Use words for auto-stemmed AND logic: `rate limit` matches both "rate" AND "limit".
2. Use `"quoted phrases"` for exact position matching: `"infinite scroll"` requires adjacent words in order.
3. Combine words and phrases for mixed queries: `middleware "rate limit"`.
4. Use multiple queries for OR logic: `queries=["authentication", "middleware"]`.

## Artisan

- Run Artisan commands directly via the command line (e.g., `php artisan route:list`). Use `php artisan list` to discover available commands and `php artisan [command] --help` to check parameters.
- Inspect routes with `php artisan route:list`. Filter with: `--method=GET`, `--name=users`, `--path=api`, `--except-vendor`, `--only-vendor`.
- Read configuration values using dot notation: `php artisan config:show app.name`, `php artisan config:show database.default`. Or read config files directly from the `config/` directory.
- To check environment variables, read the `.env` file directly.

## Tinker

- Execute PHP in app context for debugging and testing code. Do not create models without user approval, prefer tests with factories instead. Prefer existing Artisan commands over custom tinker code.
- Always use single quotes to prevent shell expansion: `php artisan tinker --execute 'Your::code();'`
  - Double quotes for PHP strings inside: `php artisan tinker --execute 'User::where("active", true)->count();'`

=== php rules ===

# PHP

- Always use curly braces for control structures, even for single-line bodies.
- Use PHP 8 constructor property promotion: `public function __construct(public GitHub $github) { }`. Do not leave empty zero-parameter `__construct()` methods unless the constructor is private.
- Use explicit return type declarations and type hints for all method parameters: `function isAccessible(User $user, ?string $path = null): bool`
- Use TitleCase for Enum keys: `FavoritePerson`, `BestLake`, `Monthly`.
- Prefer PHPDoc blocks over inline comments. Only add inline comments for exceptionally complex logic.
- Use array shape type definitions in PHPDoc blocks.

=== tests rules ===

# Test Enforcement

- Every change must be programmatically tested. Write a new test or update an existing test, then run the affected tests to make sure they pass.
- Run the minimum number of tests needed to ensure code quality and speed. Use `php artisan test --compact` with a specific filename or filter.

=== inertia-laravel/core rules ===

# Inertia

- Inertia creates fully client-side rendered SPAs without modern SPA complexity, leveraging existing server-side patterns.
- Components live in `resources/js/pages` (unless specified in `vite.config.js`). Use `Inertia::render()` for server-side routing instead of Blade views.
- ALWAYS use `search-docs` tool for version-specific Inertia documentation and updated code examples.
- IMPORTANT: Activate `inertia-react-development` when working with Inertia client-side patterns.

# Inertia v3

- Use all Inertia features from v1, v2, and v3. Check the documentation before making changes to ensure the correct approach.
- New v3 features: standalone HTTP requests (`useHttp` hook), optimistic updates with automatic rollback, layout props (`useLayoutProps` hook), instant visits, simplified SSR via `@inertiajs/vite` plugin, custom exception handling for error pages.
- Carried over from v2: deferred props, infinite scroll, merging props, polling, prefetching, once props, flash data.
- When using deferred props, add an empty state with a pulsing or animated skeleton.
- Axios has been removed. Use the built-in XHR client with interceptors, or install Axios separately if needed.
- `Inertia::lazy()` / `LazyProp` has been removed. Use `Inertia::optional()` instead.
- Prop types (`Inertia::optional()`, `Inertia::defer()`, `Inertia::merge()`) work inside nested arrays with dot-notation paths.
- SSR works automatically in Vite dev mode with `@inertiajs/vite` - no separate Node.js server needed during development.
- Event renames: `invalid` is now `httpException`, `exception` is now `networkError`.
- `router.cancel()` replaced by `router.cancelAll()`.
- The `future` configuration namespace has been removed - all v2 future options are now always enabled.

=== laravel/core rules ===

# Do Things the Laravel Way

- Use `php artisan make:` commands to create new files (i.e. migrations, controllers, models, etc.). You can list available Artisan commands using `php artisan list` and check their parameters with `php artisan [command] --help`.
- If you're creating a generic PHP class, use `php artisan make:class`.
- Pass `--no-interaction` to all Artisan commands to ensure they work without user input. You should also pass the correct `--options` to ensure correct behavior.

### Model Creation

- When creating new models, create useful factories and seeders for them too. Ask the user if they need any other things, using `php artisan make:model --help` to check the available options.

## APIs & Eloquent Resources

- For APIs, default to using Eloquent API Resources and API versioning unless existing API routes do not, then you should follow existing application convention.

## URL Generation

- When generating links to other pages, prefer named routes and the `route()` function.

## Testing

- When creating models for tests, use the factories for the models. Check if the factory has custom states that can be used before manually setting up the model.
- Faker: Use methods such as `$this->faker->word()` or `fake()->randomDigit()`. Follow existing conventions whether to use `$this->faker` or `fake()`.
- When creating tests, make use of `php artisan make:test [options] {name}` to create a feature test, and pass `--unit` to create a unit test. Most tests should be feature tests.

## Vite Error

- If you receive an "Illuminate\Foundation\ViteException: Unable to locate file in Vite manifest" error, you can run `npm run build` or ask the user to run `npm run dev` or `composer run dev`.

## Deployment

- Laravel can be deployed using [Laravel Cloud](https://cloud.laravel.com/), which is the fastest way to deploy and scale production Laravel applications.

=== octane/core rules ===

# Laravel Octane

This application uses Laravel Octane, a long-running PHP server. The application bootstraps once and handles many requests within the same process.

- Never store request-specific state in singletons or static properties, because it can leak across requests.
- Use `config('octane.server')` to detect the active driver (`swoole`, `roadrunner`, or `frankenphp`).
- Prefer scoped bindings (`$this->app->scoped()`) over singletons for per-request services.

When working on Octane-specific features (concurrency, shared tables, memory, driver configuration, testing), invoke `octane-development` for detailed rules.

=== wayfinder/core rules ===

# Laravel Wayfinder

Use Wayfinder to generate TypeScript functions for Laravel routes. Import from `@/actions/` (controllers) or `@/routes/` (named routes).

=== pint/core rules ===

# Laravel Pint Code Formatter

- If you have modified any PHP files, you must run `vendor/bin/pint --dirty --format agent` before finalizing changes to ensure your code matches the project's expected style.
- Do not run `vendor/bin/pint --test --format agent`, simply run `vendor/bin/pint --format agent` to fix any formatting issues.

=== pest/core rules ===

## Pest

- This project uses Pest for testing. Create tests: `php artisan make:test --pest {name}`.
- Run tests: `php artisan test --compact` or filter: `php artisan test --compact --filter=testName`.
- Do NOT delete tests without approval.

=== inertia-react/core rules ===

# Inertia + React

- IMPORTANT: Activate `inertia-react-development` when working with Inertia React client-side patterns.

</laravel-boost-guidelines>
