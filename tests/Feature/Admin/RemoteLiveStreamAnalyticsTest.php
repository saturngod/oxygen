<?php

use App\Enums\LiveStreamViewerPeriod;
use App\Models\LiveStream;
use App\Models\Organization;
use App\Services\RemoteLiveStreamAnalytics;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;

uses(RefreshDatabase::class);

test('remote analytics maps nullable points and never exposes query credentials', function () {
    $organization = Organization::factory()->create();
    $liveStream = LiveStream::factory()->for($organization)->create();

    config([
        'services.analytics.url' => 'http://analytics.test',
        'services.analytics.query_token' => 'private-query-token',
    ]);
    Http::fake([
        'http://analytics.test/*' => Http::response([
            'range_start' => '2026-08-09T00:00:00Z',
            'range_end' => '2026-08-10T00:00:00Z',
            'coverage_start' => '2026-08-09T12:00:00Z',
            'timezone' => 'UTC',
            'granularity' => 'hour',
            'points' => [
                ['timestamp' => '2026-08-09T00:00:00Z', 'value' => null, 'complete' => true],
                ['timestamp' => '2026-08-09T12:00:00Z', 'value' => 4, 'complete' => false],
            ],
            'summary' => [
                'peak_viewers' => 4,
                'broadcasts' => 1,
                'viewer_identity_additions' => 2,
                'playback_requests' => 3,
            ],
        ]),
    ]);

    $analytics = app(RemoteLiveStreamAnalytics::class)->build($liveStream, LiveStreamViewerPeriod::Day);

    expect($analytics['available'])->toBeTrue()
        ->and($analytics['points'][0]['value'])->toBeNull()
        ->and($analytics['points'][1]['value'])->toBe(4)
        ->and($analytics['points'][1]['complete'])->toBeFalse();

    Http::assertSent(fn ($request): bool => $request->hasHeader('Authorization', 'Bearer private-query-token')
        && ! str_contains($request->body(), 'private-query-token'));
});

test('remote analytics returns an unavailable nullable chart after bounded retries', function () {
    $organization = Organization::factory()->create();
    $liveStream = LiveStream::factory()->for($organization)->create();

    config([
        'services.analytics.url' => 'http://analytics.test',
        'services.analytics.query_token' => 'private-query-token',
    ]);
    Http::fake([
        'http://analytics.test/*' => Http::response(['error' => 'down'], 503),
    ]);

    $analytics = app(RemoteLiveStreamAnalytics::class)->build($liveStream, LiveStreamViewerPeriod::Year);

    expect($analytics['available'])->toBeFalse()
        ->and($analytics['points'])->toHaveCount(12)
        ->and($analytics['points'][0]['value'])->toBeNull();

    Http::assertSentCount(3);
});
