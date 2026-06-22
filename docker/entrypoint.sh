#!/usr/bin/env bash
set -euo pipefail

cd /app

# APP_KEY must be provided via Dokploy env. Do NOT auto-generate: there is no
# persisted .env in the image, and a fresh key per boot would make every
# encrypted LiveStream::stream_key undecryptable and invalidate all sessions.
if [ -z "${APP_KEY:-}" ]; then
    echo "[entrypoint] FATAL: APP_KEY is not set. Set it via Dokploy env (php artisan key:generate --show)." >&2
    exit 1
fi

# Run DB migrations on boot. This ASSUMES A SINGLE REPLICA — concurrent
# migrations will race if you scale to 2+ containers. If you scale out, set
# RUN_MIGRATIONS=false here and run migrations as a one-shot deploy step.
if [ "${RUN_MIGRATIONS:-true}" = "true" ]; then
    echo "[entrypoint] Running migrations..."
    php artisan migrate --force
fi

# Composer ran with --no-scripts, so build the package manifest now.
php artisan package:discover --ansi

# Cache config, routes, views, events for production performance.
echo "[entrypoint] Caching framework config..."
php artisan config:cache
php artisan route:cache
php artisan view:cache
php artisan event:cache

# Hand off to the CMD (supervisord).
exec "$@"
