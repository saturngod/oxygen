<?php

use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerHourlyRollup;
use App\Models\LiveStreamViewerRollup;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('it deletes rollups older than the retention window and keeps recent ones', function () {
    $old = LiveStreamViewerRollup::factory()->create([
        'minute' => now()->subDays(40)->startOfMinute(),
        'current_viewers' => 12,
        'peak_viewers' => 17,
        'viewer_identity_additions' => 8,
        'playlist_requests_delta' => 30,
        'segment_requests_delta' => 70,
    ]);

    $recent = LiveStreamViewerRollup::factory()->create([
        'minute' => now()->subDays(5)->startOfMinute(),
    ]);

    $this->artisan('rollups:prune', ['--days' => 30])->assertSuccessful();

    expect(LiveStreamViewerRollup::query()->whereKey($old->id)->exists())->toBeFalse();
    expect(LiveStreamViewerRollup::query()->whereKey($recent->id)->exists())->toBeTrue();

    $hourly = LiveStreamViewerHourlyRollup::query()
        ->where('live_stream_id', $old->live_stream_id)
        ->sole();

    expect($hourly->bucket_start->equalTo($old->minute->startOfHour()))->toBeTrue()
        ->and($hourly->peak_viewers)->toBe(17)
        ->and($hourly->viewer_identity_additions)->toBe(8)
        ->and($hourly->playlist_requests)->toBe(30)
        ->and($hourly->segment_requests)->toBe(70);
});

test('it never deletes session summaries', function () {
    $session = LiveStreamSession::factory()->create();

    LiveStreamViewerRollup::factory()->create([
        'minute' => now()->subDays(90)->startOfMinute(),
    ]);

    $this->artisan('rollups:prune', ['--days' => 30])->assertSuccessful();

    expect(LiveStreamSession::query()->whereKey($session->id)->exists())->toBeTrue();
});

test('it rejects a non-positive days option', function () {
    LiveStreamViewerRollup::factory()->create([
        'minute' => now()->subDays(90)->startOfMinute(),
    ]);

    $this->artisan('rollups:prune', ['--days' => 0])->assertFailed();

    expect(LiveStreamViewerRollup::query()->count())->toBe(1);
});
