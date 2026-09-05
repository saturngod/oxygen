# syntax=docker/dockerfile:1.7

# ─────────────────────────────────────────────────────────────
# Oxygen — single-image production build for Dokploy
# Runs in one container via supervisord:
#   - Laravel Octane (FrankenPHP)   :8000  (HTTP / control plane)
#   - Laravel queue worker          (queued jobs)
#   - Laravel webhook consumer      (raw Redis events → queued jobs)
#   - Laravel scheduler             (daily rollup prune, etc.)
#   - golang-queue VOD worker       (ffmpeg → HLS → S3)
#   - golang-live service           :8081 (HTTP/HLS) + :1935 (RTMP ingest)
#   - golang-analytics API          127.0.0.1:8090 (container-internal only)
#
# Durable application state lives in Postgres, Redis, S3, and the explicitly
# mounted public-upload/live-callback volumes described below.
# ─────────────────────────────────────────────────────────────


# ── Stage 1: Node runtime donor ───────────────────────────────
FROM node:24-bookworm-slim AS node-runtime


# ── Stage 2: PHP/Composer dependencies ───────────────────────
FROM composer:2 AS vendor
WORKDIR /app

COPY composer.json composer.lock ./
# Install prod deps only; skip scripts (artisan not fully available yet).
RUN composer install \
    --no-dev \
    --no-interaction \
    --no-scripts \
    --prefer-dist \
    --optimize-autoloader


# ── Stage 3: build frontend assets with PHP + Wayfinder ───────
# The Wayfinder Vite plugin runs `php artisan wayfinder:generate` during every
# build. Use a PHP 8.4 image with Composer dependencies present, then copy the
# Node runtime into it; a Node-only stage cannot execute the Artisan command.
FROM dunglas/frankenphp:1-php8.4-bookworm AS assets
COPY --from=node-runtime /usr/local /usr/local
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
COPY --from=vendor /app/vendor ./vendor
RUN mkdir -p bootstrap/cache \
    && php artisan package:discover --ansi \
    && npm run build


# ── Stage 4: build the Go services ──────────────────────────
FROM golang:1.25.13-bookworm AS go-build
WORKDIR /src

# golang-queue (VOD transcode worker)
COPY golang-queue/go.mod golang-queue/go.sum ./golang-queue/
RUN cd golang-queue && go mod download
COPY golang-queue ./golang-queue
RUN cd golang-queue && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oxygen-queue ./cmd/worker

# golang-live (RTMP ingest + live HLS)
COPY golang-live/go.mod golang-live/go.sum ./golang-live/
RUN cd golang-live && go mod download
COPY golang-live ./golang-live
RUN cd golang-live && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oxygen-live ./cmd/live

# golang-analytics (isolated analytics ingestion/query API)
COPY golang-analytics/go.mod golang-analytics/go.sum ./golang-analytics/
RUN cd golang-analytics && go mod download
COPY golang-analytics ./golang-analytics
RUN cd golang-analytics && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oxygen-analytics ./cmd/analytics


# ── Stage 5: optional analytics-only runtime image ────────────────────
FROM debian:bookworm-slim AS analytics-runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system analytics \
    && useradd --system --gid analytics --home-dir /app --shell /usr/sbin/nologin analytics

WORKDIR /app
COPY --from=go-build /out/oxygen-analytics /usr/local/bin/oxygen-analytics
COPY --chown=analytics:analytics golang-analytics/migrations ./migrations

ENV ANALYTICS_MIGRATIONS_PATH=/app/migrations

USER analytics
EXPOSE 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
    CMD curl --fail --silent http://127.0.0.1:8090/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/oxygen-analytics"]
CMD ["serve"]


# ── Stage 6: final Oxygen runtime image ──────────────────────
FROM dunglas/frankenphp:1-php8.4-bookworm AS runtime

# System deps: ffmpeg (transcode worker), supervisor (process mgr).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
        supervisor \
        passwd \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*

# PHP extensions required by Laravel + Octane + Postgres/Redis.
RUN install-php-extensions \
        pcntl \
        pdo_pgsql \
        pgsql \
        bcmath \
        gd \
        intl \
        zip \
        exif \
        opcache \
        redis

RUN groupadd --system oxygen \
    && useradd --system --gid oxygen --home-dir /app --shell /usr/sbin/nologin oxygen

WORKDIR /app

# Application code (built assets + vendor merged in).
COPY --chown=oxygen:oxygen . .
COPY --chown=oxygen:oxygen --from=vendor /app/vendor ./vendor
COPY --chown=oxygen:oxygen --from=assets /app/public/build ./public/build

# Go service binaries.
COPY --from=go-build /out/oxygen-queue /usr/local/bin/oxygen-queue
COPY --from=go-build /out/oxygen-live /usr/local/bin/oxygen-live
COPY --from=go-build /out/oxygen-analytics /usr/local/bin/oxygen-analytics

# Production PHP config + opcache tuning.
COPY docker/php.ini "$PHP_INI_DIR/conf.d/zz-oxygen.ini"

# Process supervision + startup.
COPY docker/supervisord.conf /etc/supervisor/conf.d/oxygen.conf
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY docker/healthcheck.sh /usr/local/bin/healthcheck.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/healthcheck.sh

# Laravel and Go writable directories. The relative storage symlink remains
# valid when /app/storage/app/public is mounted as a Dokploy volume.
ENV WORK_DIR=/tmp/transcoder \
    LIVE_HLS_ROOT=/var/lib/oxygen-live/hls \
    LIVE_CALLBACK_ROOT=/var/lib/oxygen-live/callbacks \
    ANALYTICS_OUTBOX_ROOT=/var/lib/oxygen-live/analytics-outbox \
    ANALYTICS_ADDR=127.0.0.1:8090 \
    ANALYTICS_URL=http://127.0.0.1:8090 \
    ANALYTICS_MIGRATIONS_PATH=/app/golang-analytics/migrations

RUN mkdir -p \
        storage/app/public \
        storage/framework/cache \
        storage/framework/sessions \
        storage/framework/views \
        storage/logs \
        bootstrap/cache \
        /tmp/transcoder \
        /var/lib/oxygen-live/hls \
        /var/lib/oxygen-live/callbacks \
        /var/lib/oxygen-live/analytics-outbox \
    && rm -rf public/storage \
    && ln -s ../storage/app/public public/storage \
    && chown -R oxygen:oxygen \
        storage bootstrap/cache /tmp/transcoder /var/lib/oxygen-live \
    && chmod -R ug+rwX storage bootstrap/cache /tmp/transcoder /var/lib/oxygen-live

# Configure named Dokploy volumes for public organization images and the live
# callback outbox. Transcode scratch is intentionally a separate sized volume
# (~15GB x WORKER_CONCURRENCY) and is cleaned after each successful job.
VOLUME ["/app/storage/app/public", "/var/lib/oxygen-live/callbacks", "/var/lib/oxygen-live/analytics-outbox", "/tmp/transcoder"]

# Ports: 8000 web/Octane, 8081 live HTTP/HLS, 1935 RTMP ingest.
EXPOSE 8000 8081 1935

# Require every supervised process plus Laravel, live, and analytics readiness
# endpoints. This lets Dokploy reject a release where a background process is
# crash-looping even though Octane still answers requests.
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/healthcheck.sh"]

# Dokploy must use one replica with a stop-first update and a StopGracePeriod
# of at least 86400 seconds so the supervised VOD drain window is honored.
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["supervisord", "-c", "/etc/supervisor/conf.d/oxygen.conf", "-n"]
