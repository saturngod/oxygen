<?php

test('the production container runs analytics internally', function () {
    $root = dirname(__DIR__, 2);
    $dockerfile = file_get_contents($root.'/Dockerfile');
    $supervisor = file_get_contents($root.'/docker/supervisord.conf');
    $healthcheck = file_get_contents($root.'/docker/healthcheck.sh');
    $entrypoint = file_get_contents($root.'/docker/entrypoint.sh');
    $compose = file_get_contents($root.'/compose.yaml');
    $environment = file_get_contents($root.'/docker/local.env.example');

    expect($dockerfile)
        ->toContain('COPY --from=go-build /out/oxygen-analytics /usr/local/bin/oxygen-analytics')
        ->toContain('ANALYTICS_ADDR=127.0.0.1:8090')
        ->toContain('ANALYTICS_URL=http://127.0.0.1:8090')
        ->toContain('ANALYTICS_MIGRATIONS_PATH=/app/golang-analytics/migrations')
        ->toContain('XDG_CONFIG_HOME=/config')
        ->toContain('XDG_DATA_HOME=/data')
        ->and($supervisor)
        ->toContain('[program:go-analytics]')
        ->toContain('command=/usr/local/bin/oxygen-analytics serve')
        ->and($healthcheck)
        ->toContain('go-analytics')
        ->toContain('http://127.0.0.1:8090/readyz')
        ->and($entrypoint)
        ->toContain('"${XDG_CONFIG_HOME:-/config}/caddy"')
        ->toContain('"${XDG_DATA_HOME:-/data}/caddy"')
        ->and($compose)
        ->not->toContain("\n  analytics-api:")
        ->toContain("      analytics-postgres:\n        condition: service_healthy")
        ->and($environment)
        ->toContain('ANALYTICS_ADDR=127.0.0.1:8090')
        ->toContain('ANALYTICS_URL=http://127.0.0.1:8090');
});
