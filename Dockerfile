# syntax=docker/dockerfile:1.7

# ─────────────────────────────────────────────────────────────
# Oxygen — single-image production build for Dokploy
# Runs in one container via supervisord:
#   - Laravel Octane (FrankenPHP)   :8000  (HTTP / control plane)
#   - Laravel queue worker          (webhooks + jobs)
#   - Laravel scheduler             (daily rollup prune, etc.)
#   - golang-queue VOD worker       (ffmpeg → HLS → S3)
#   - golang-live service           :8081 (HTTP/HLS) + :1935 (RTMP ingest)
#
# All persistent state lives in external services (Postgres, Redis, S3)
# provided through environment variables — no rustfs/local object store.
# ─────────────────────────────────────────────────────────────


# ── Stage 1: build frontend assets ───────────────────────────
FROM node:24-bookworm-slim AS assets
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

# Vite needs the app sources + (some configs read vendor wayfinder output,
# which is committed under resources/js), so copy the whole tree.
COPY . .
RUN npm run build


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


# ── Stage 3: build the two Go services ───────────────────────
FROM golang:1.25-bookworm AS go-build
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


# ── Stage 4: final runtime image ─────────────────────────────
FROM dunglas/frankenphp:1-php8.4-bookworm AS runtime

# System deps: ffmpeg (transcode worker), supervisor (process mgr).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
        supervisor \
        ca-certificates \
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

WORKDIR /app

# Application code (built assets + vendor merged in).
COPY . .
COPY --from=vendor /app/vendor ./vendor
COPY --from=assets /app/public/build ./public/build

# Go service binaries.
COPY --from=go-build /out/oxygen-queue /usr/local/bin/oxygen-queue
COPY --from=go-build /out/oxygen-live /usr/local/bin/oxygen-live

# Production PHP config + opcache tuning.
COPY docker/php.ini "$PHP_INI_DIR/conf.d/zz-oxygen.ini"

# Process supervision + startup.
COPY docker/supervisord.conf /etc/supervisor/conf.d/oxygen.conf
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Laravel writable dirs (FrankenPHP runs as root by default here, but keep
# permissions sane for the storage/cache trees).
RUN mkdir -p storage/framework/{cache,sessions,views} storage/logs bootstrap/cache \
    && chmod -R 775 storage bootstrap/cache

# Ports: 8000 web/Octane, 8081 live HTTP/HLS, 1935 RTMP ingest.
EXPOSE 8000 8081 1935

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["supervisord", "-c", "/etc/supervisor/conf.d/oxygen.conf", "-n"]
