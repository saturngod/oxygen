<?php

use App\Enums\LiveStreamSessionStatus;
use App\Enums\LiveStreamStatus;
use App\Enums\OrganizationRole;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Http;

uses(RefreshDatabase::class);

function liveStreamAdmin(): array
{
    $user = User::factory()->create(['email_verified_at' => now()]);
    $organization = Organization::factory()->create();

    $organization->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    return [$user, $organization];
}

test('admin can create and view a live stream with encrypted stream key', function () {
    $this->withoutVite();

    [$user, $organization] = liveStreamAdmin();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.store', $organization), [
            'title' => 'Launch Stream',
            'recording_enabled' => '1',
        ])
        ->assertRedirect();

    $liveStream = LiveStream::query()->where('organization_id', $organization->id)->sole();

    expect($liveStream->title)->toBe('Launch Stream')
        ->and($liveStream->recording_enabled)->toBeTrue()
        ->and($liveStream->rtmp_url)->toBe('rtmp://127.0.0.1:1935/live')
        ->and($liveStream->hls_url)->toContain($liveStream->public_id);

    $storedKey = DB::table('live_streams')->whereKey($liveStream->id)->value('stream_key');
    expect($storedKey)->not->toBe($liveStream->stream_key);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.show', [$organization, $liveStream]))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->component('admin/live-streams/show')
            ->where('liveStream.title', 'Launch Stream')
            ->where('liveStream.stream_key', $liveStream->stream_key)
        );
});

test('operator cannot manage live streams', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $organization = Organization::factory()->create();

    $organization->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.index', $organization))
        ->assertForbidden();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.store', $organization), [
            'title' => 'Blocked',
        ])
        ->assertForbidden();
});

test('admin can list live streams with latest session stats', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->create();

    LiveStreamSession::factory()
        ->for($liveStream)
        ->create([
            'status' => LiveStreamSessionStatus::Ended,
            'current_viewers' => 0,
            'peak_viewers' => 3,
            'started_at' => now()->subHour(),
        ]);

    LiveStreamSession::factory()
        ->for($liveStream)
        ->create([
            'status' => LiveStreamSessionStatus::Live,
            'current_viewers' => 12,
            'peak_viewers' => 18,
            'started_at' => now(),
        ]);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.index', $organization))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->component('admin/live-streams/index')
            ->where('liveStreams.0.current_viewers', 12)
            ->where('liveStreams.0.peak_viewers', 18)
        );
});

test('recording changes on a live stream require restart', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create([
            'recording_enabled' => false,
            'settings_version' => 3,
        ]);

    $this->actingAs($user)
        ->put(route('admin.organizations.live-streams.update', [$organization, $liveStream]), [
            'title' => $liveStream->title,
            'recording_enabled' => '1',
        ])
        ->assertRedirect(route('admin.organizations.live-streams.show', [$organization, $liveStream]));

    $liveStream->refresh();

    expect($liveStream->recording_enabled)->toBeTrue()
        ->and($liveStream->restart_required)->toBeTrue()
        ->and($liveStream->settings_version)->toBe(4);
});

test('rotating a live stream key requires restart', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create(['settings_version' => 1]);

    $oldKey = $liveStream->stream_key;

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.rotate-key', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.show', [$organization, $liveStream]));

    $liveStream->refresh();

    expect($liveStream->stream_key)->not->toBe($oldKey)
        ->and($liveStream->restart_required)->toBeTrue()
        ->and($liveStream->settings_version)->toBe(2);
});

test('disabling a live stream kicks the active publisher', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create();

    config([
        'services.live.control_url' => 'http://live-service.test',
        'services.live.control_token' => 'control-secret',
    ]);

    Http::fake([
        'http://live-service.test/streams/'.$liveStream->public_id.'/restart' => Http::response(['ok' => true]),
    ]);

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.disable', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.index', $organization));

    $liveStream->refresh();

    expect($liveStream->status)->toBe(LiveStreamStatus::Disabled);

    Http::assertSentCount(1);
});

test('disabling an idle live stream does not call the control service', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->create(['status' => LiveStreamStatus::Idle]);

    config([
        'services.live.control_url' => 'http://live-service.test',
        'services.live.control_token' => 'control-secret',
    ]);

    Http::fake();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.disable', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.index', $organization));

    expect($liveStream->refresh()->status)->toBe(LiveStreamStatus::Disabled);

    Http::assertNothingSent();
});

test('restart calls live control service and marks stream restarting', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create(['restart_required' => true]);

    config([
        'services.live.control_url' => 'http://live-service.test',
        'services.live.control_token' => 'control-secret',
    ]);

    Http::fake([
        'http://live-service.test/streams/'.$liveStream->public_id.'/restart' => Http::response(['ok' => true]),
    ]);

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.restart', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.show', [$organization, $liveStream]));

    $liveStream->refresh();

    expect($liveStream->status)->toBe(LiveStreamStatus::Restarting)
        ->and($liveStream->restart_required)->toBeFalse();

    Http::assertSentCount(1);
});
