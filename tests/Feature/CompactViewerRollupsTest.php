<?php

use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerHourlyRollup;
use App\Models\LiveStreamViewerRollup;
use App\Models\Organization;
use Carbon\CarbonImmutable;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('it compacts complete UTC hours across sessions and safely repairs recent hours', function () {
    $this->travelTo(CarbonImmutable::parse('2026-08-09T12:30:00Z'));

    $organization = Organization::factory()->create();
    $liveStream = LiveStream::factory()->for($organization)->create();
    $firstSession = LiveStreamSession::factory()->for($liveStream)->create();
    $secondSession = LiveStreamSession::factory()->for($liveStream)->create();

    foreach ([
        [$firstSession, '2026-08-09T10:59:00Z', 5, 7, 3, 10, 20, 2],
        [$secondSession, '2026-08-09T10:59:30Z', 9, 9, 4, 11, 21, 1],
        [$firstSession, '2026-08-09T11:00:00Z', 4, 6, 2, 12, 22, 3],
    ] as [$session, $minute, $current, $peak, $identities, $playlists, $segments, $samples]) {
        LiveStreamViewerRollup::factory()->create([
            'organization_id' => $organization->id,
            'live_stream_id' => $liveStream->id,
            'live_stream_session_id' => $session->id,
            'minute' => $minute,
            'current_viewers' => $current,
            'peak_viewers' => $peak,
            'viewer_identity_additions' => $identities,
            'playlist_requests_delta' => $playlists,
            'segment_requests_delta' => $segments,
            'sample_count' => $samples,
        ]);
    }

    $this->artisan('rollups:compact-hourly', ['--hours' => 3])->assertSuccessful();

    $tenOClock = LiveStreamViewerHourlyRollup::query()
        ->where('live_stream_id', $liveStream->id)
        ->where('bucket_start', '2026-08-09 10:00:00')
        ->sole();

    expect($tenOClock->peak_viewers)->toBe(9)
        ->and($tenOClock->viewer_identity_additions)->toBe(7)
        ->and($tenOClock->playlist_requests)->toBe(21)
        ->and($tenOClock->segment_requests)->toBe(41)
        ->and($tenOClock->sample_count)->toBe(3)
        ->and(LiveStreamViewerHourlyRollup::query()->count())->toBe(2);

    LiveStreamViewerRollup::factory()->create([
        'organization_id' => $organization->id,
        'live_stream_id' => $liveStream->id,
        'live_stream_session_id' => $secondSession->id,
        'minute' => '2026-08-09T11:45:00Z',
        'current_viewers' => 12,
        'peak_viewers' => 14,
        'viewer_identity_additions' => 5,
        'playlist_requests_delta' => 15,
        'segment_requests_delta' => 25,
        'sample_count' => 2,
    ]);

    $this->artisan('rollups:compact-hourly', ['--hours' => 3])->assertSuccessful();

    $elevenOClock = LiveStreamViewerHourlyRollup::query()
        ->where('live_stream_id', $liveStream->id)
        ->where('bucket_start', '2026-08-09 11:00:00')
        ->sole();

    expect(LiveStreamViewerHourlyRollup::query()->count())->toBe(2)
        ->and($elevenOClock->peak_viewers)->toBe(14)
        ->and($elevenOClock->viewer_identity_additions)->toBe(7)
        ->and($elevenOClock->playlist_requests)->toBe(27)
        ->and($elevenOClock->segment_requests)->toBe(47)
        ->and($elevenOClock->sample_count)->toBe(5);
});

test('it rejects a non-positive compaction window', function () {
    $this->artisan('rollups:compact-hourly', ['--hours' => 0])->assertFailed();
});
