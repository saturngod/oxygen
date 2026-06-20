<?php

use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerRollup;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('it deletes rollups older than the retention window and keeps recent ones', function () {
    $old = LiveStreamViewerRollup::factory()->create([
        'minute' => now()->subDays(40)->startOfMinute(),
    ]);

    $recent = LiveStreamViewerRollup::factory()->create([
        'minute' => now()->subDays(5)->startOfMinute(),
    ]);

    $this->artisan('rollups:prune', ['--days' => 30])->assertSuccessful();

    expect(LiveStreamViewerRollup::query()->whereKey($old->id)->exists())->toBeFalse();
    expect(LiveStreamViewerRollup::query()->whereKey($recent->id)->exists())->toBeTrue();
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
