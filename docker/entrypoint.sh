#!/usr/bin/env bash
set -euo pipefail

cd /app

# Docker creates fresh named-volume roots as root. Recreate the required paths,
# storage symlink, and ownership before starting non-root supervised processes.
mkdir -p \
    storage/app/public \
    storage/framework/cache \
    storage/framework/sessions \
    storage/framework/views \
    storage/logs \
    bootstrap/cache \
    "${WORK_DIR:-/tmp/transcoder}" \
    "${LIVE_HLS_ROOT:-/var/lib/oxygen-live/hls}" \
    "${LIVE_CALLBACK_ROOT:-/var/lib/oxygen-live/callbacks}"

rm -rf public/storage
ln -s ../storage/app/public public/storage

chown -R oxygen:oxygen \
    storage \
    bootstrap/cache \
    "${WORK_DIR:-/tmp/transcoder}" \
    "${LIVE_HLS_ROOT:-/var/lib/oxygen-live/hls}" \
    "${LIVE_CALLBACK_ROOT:-/var/lib/oxygen-live/callbacks}"

# APP_KEY must be provided via Dokploy env. Do NOT auto-generate: there is no
# persisted .env in the image, and a fresh key per boot would make every
# encrypted LiveStream::stream_key undecryptable and invalidate all sessions.
if [ -z "${APP_KEY:-}" ]; then
    echo "[entrypoint] FATAL: APP_KEY is not set. Set it via Dokploy env (php artisan key:generate --show)." >&2
    exit 1
fi

# Composer ran with --no-scripts, so build the package manifest before running
# any other Artisan command.
php artisan package:discover --ansi

# Run migrations under an atomic cache lock. Keep RUN_MIGRATIONS enabled on one
# container only when scaling; a dedicated one-shot deploy task is preferred.
if [ "${RUN_MIGRATIONS:-true}" = "true" ]; then
    echo "[entrypoint] Running migrations..."
    php artisan migrate --isolated --force
fi

# Cache config, routes, views, events for production performance.
echo "[entrypoint] Caching framework config..."
php artisan config:cache
php artisan route:cache
php artisan view:cache
php artisan event:cache

chown -R oxygen:oxygen storage bootstrap/cache

# Hand off to the CMD (supervisord).
exec "$@"
