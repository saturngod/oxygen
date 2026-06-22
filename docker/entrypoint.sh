#!/usr/bin/env bash
set -euo pipefail

cd /app

# Generate APP_KEY only if not provided (recommended: set APP_KEY via Dokploy env).
if [ -z "${APP_KEY:-}" ]; then
    echo "[entrypoint] APP_KEY not set — generating an ephemeral one."
    php artisan key:generate --force
fi

# Run DB migrations on boot. Safe to run repeatedly; only one container should
# typically own this — fine for a single-instance Dokploy deploy.
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
