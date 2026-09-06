<?php

use App\Enums\LiveStreamSessionStatus;
use App\Enums\LiveStreamStatus;
use App\Enums\OrganizationRole;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerHourlyRollup;
use App\Models\LiveStreamViewerRollup;
use App\Models\Organization;
use App\Models\Profile;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\Client\Request;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Http;

uses(RefreshDatabase::class);

beforeEach(function () {
    $this->withoutVite();
});

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
    $profile = Profile::factory()->for($organization)->create();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.store', $organization), [
            'title' => 'Launch Stream',
            'profile_id' => $profile->id,
        ])
        ->assertRedirect();

    $liveStream = LiveStream::query()->where('organization_id', $organization->id)->sole();

    expect($liveStream->title)->toBe('Launch Stream')
        ->and($liveStream->profile_id)->toBe($profile->id)
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

test('remote analytics metrics do not replace control-plane playback settings', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create(['settings_version' => 7]);
    $session = LiveStreamSession::factory()
        ->for($liveStream)
        ->create([
            'status' => LiveStreamSessionStatus::Live,
            'settings_version' => 7,
            'hls_url' => 'https://stream.example/live/index.m3u8',
        ]);
    $liveStream->forceFill(['active_session_id' => $session->id])->save();

    config([
        'services.analytics.url' => 'http://analytics.test',
        'services.analytics.query_token' => 'private-query-token',
    ]);
    Http::fake([
        'http://analytics.test/*' => Http::response([
            'session' => [
                'session_id' => $session->id,
                'status' => 'live',
                'current_viewers' => 18,
                'peak_viewers' => 23,
                'unique_viewers' => 11,
                'playlist_requests' => 40,
                'segment_requests' => 80,
            ],
        ]),
    ]);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.show', [$organization, $liveStream]))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->where('liveStream.settings_version', 7)
            ->where('liveStream.current_session.settings_version', 7)
            ->where('liveStream.current_session.hls_url', 'https://stream.example/live/index.m3u8')
            ->where('liveStream.current_session.current_viewers', 18)
            ->where('liveStream.current_session.peak_viewers', 23)
        );
});

test('live stream creation only accepts a profile from the organization', function () {
    [$user, $organization] = liveStreamAdmin();
    $foreignProfile = Profile::factory()->create();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.store', $organization), [
            'title' => 'Invalid profile',
            'profile_id' => $foreignProfile->id,
        ])
        ->assertSessionHasErrors('profile_id');

    expect(LiveStream::query()->count())->toBe(0);
});

test('live stream create page lists profiles with the default first', function () {
    [$user, $organization] = liveStreamAdmin();
    Profile::factory()->for($organization)->create(['name' => 'Secondary']);
    $default = Profile::factory()->for($organization)->create([
        'name' => 'Live Adaptive',
        'qualities' => ['360p', '720p'],
        'is_default' => true,
    ]);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.create', $organization))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->component('admin/live-streams/create')
            ->has('profiles', 2)
            ->where('profiles.0.id', $default->id)
            ->where('profiles.0.qualities', ['360p', '720p'])
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

test('admin can view hourly viewer analytics with peak values per bucket', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create();

    $session = LiveStreamSession::factory()
        ->for($liveStream)
        ->create([
            'unique_viewers' => 9,
            'playlist_requests' => 40,
            'segment_requests' => 60,
        ]);

    $liveStream->forceFill(['active_session_id' => $session->id])->save();

    LiveStreamViewerRollup::factory()->create([
        'organization_id' => $organization->id,
        'live_stream_id' => $liveStream->id,
        'live_stream_session_id' => $session->id,
        'minute' => now()->subHour()->startOfHour()->addMinutes(10),
        'current_viewers' => 7,
        'peak_viewers' => 7,
        'viewer_identity_additions' => 4,
        'playlist_requests_delta' => 15,
        'segment_requests_delta' => 25,
    ]);

    LiveStreamViewerRollup::factory()->create([
        'organization_id' => $organization->id,
        'live_stream_id' => $liveStream->id,
        'live_stream_session_id' => $session->id,
        'minute' => now()->startOfHour()->addMinutes(5),
        'current_viewers' => 12,
        'peak_viewers' => 12,
        'viewer_identity_additions' => 5,
        'playlist_requests_delta' => 25,
        'segment_requests_delta' => 35,
    ]);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.viewer', [$organization, $liveStream]))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->component('admin/live-streams/viewer')
            ->where('period', 'day')
            ->where('analytics.granularity', 'hour')
            ->has('analytics.points', 24)
            ->where('analytics.points.22.value', 7)
            ->where('analytics.points.23.value', 12)
            ->where('analytics.summary.peak_viewers', 12)
            ->where('analytics.summary.broadcasts', 1)
            ->where('analytics.summary.viewer_identity_additions', 9)
            ->where('analytics.summary.playback_requests', 100)
            ->missing('liveStream.stream_key')
            ->missing('liveStream.rtmp_url')
        );
});

test('monthly viewer analytics uses the peak minute sample for each day', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()->for($organization)->create();
    $session = LiveStreamSession::factory()->for($liveStream)->create();
    $sampleDay = now()->subDays(20)->startOfDay();

    foreach ([8, 14] as $index => $viewers) {
        LiveStreamViewerRollup::factory()->create([
            'organization_id' => $organization->id,
            'live_stream_id' => $liveStream->id,
            'live_stream_session_id' => $session->id,
            'minute' => $sampleDay->addHours($index + 1),
            'current_viewers' => $viewers,
            'peak_viewers' => $viewers,
        ]);
    }

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.viewer', [
            'organization' => $organization,
            'liveStream' => $liveStream,
            'period' => 'month',
        ]))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->component('admin/live-streams/viewer')
            ->where('period', 'month')
            ->where('analytics.granularity', 'day')
            ->has('analytics.points', 30)
            ->where('analytics.points.9.value', 14)
            ->where('analytics.summary.peak_viewers', 14)
        );
});

test('yearly viewer analytics uses durable hourly rollups and counts overlapping broadcasts', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()->for($organization)->create();

    LiveStreamSession::factory()->for($liveStream)->create([
        'started_at' => now()->subYears(2),
        'ended_at' => null,
    ]);

    LiveStreamViewerHourlyRollup::factory()->for($liveStream)->create([
        'organization_id' => $organization->id,
        'bucket_start' => now()->subMonthsNoOverflow(10)->startOfMonth()->addDay()->startOfHour(),
        'peak_viewers' => 31,
        'viewer_identity_additions' => 18,
        'playlist_requests' => 70,
        'segment_requests' => 130,
    ]);

    LiveStreamViewerHourlyRollup::factory()->for($liveStream)->create([
        'organization_id' => $organization->id,
        'bucket_start' => now()->startOfMonth()->addDay()->startOfHour(),
        'peak_viewers' => 9,
        'viewer_identity_additions' => 6,
        'playlist_requests' => 20,
        'segment_requests' => 30,
    ]);

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.viewer', [
            'organization' => $organization,
            'liveStream' => $liveStream,
            'period' => 'year',
        ]))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->where('period', 'year')
            ->where('analytics.granularity', 'month')
            ->has('analytics.points', 12)
            ->where('analytics.points.1.value', 31)
            ->where('analytics.points.11.value', 9)
            ->where('analytics.summary.peak_viewers', 31)
            ->where('analytics.summary.broadcasts', 1)
            ->where('analytics.summary.viewer_identity_additions', 24)
            ->where('analytics.summary.playback_requests', 250)
        );
});

test('viewer analytics rejects invalid periods and scopes streams to the organization', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()->for($organization)->create();
    $otherOrganization = Organization::factory()->create();
    $otherStream = LiveStream::factory()->for($otherOrganization)->create();

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.viewer', [
            'organization' => $organization,
            'liveStream' => $liveStream,
            'period' => 'week',
        ]))
        ->assertSessionHasErrors('period');

    $this->actingAs($user)
        ->get(route('admin.organizations.live-streams.viewer', [$organization, $otherStream]))
        ->assertNotFound();
});

test('admin can update a live stream title without requiring restart', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create([
            'settings_version' => 3,
        ]);

    $this->actingAs($user)
        ->put(route('admin.organizations.live-streams.update', [$organization, $liveStream]), [
            'title' => 'Updated title',
        ])
        ->assertRedirect(route('admin.organizations.live-streams.show', [$organization, $liveStream]));

    $liveStream->refresh();

    expect($liveStream->title)->toBe('Updated title')
        ->and($liveStream->restart_required)->toBeFalse()
        ->and($liveStream->settings_version)->toBe(3);
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

test('disabling an idle live stream disconnects a publisher that may still be starting', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->create(['status' => LiveStreamStatus::Idle]);

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

    expect($liveStream->refresh()->status)->toBe(LiveStreamStatus::Disabled);

    Http::assertSentCount(1);
    Http::assertSent(fn (Request $request): bool => $request->method() === 'POST'
        && $request->url() === 'http://live-service.test/streams/'.$liveStream->public_id.'/restart'
        && $request->hasHeader('Authorization', 'Bearer control-secret'));
});

test('admin can enable a disabled live stream', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->create(['status' => LiveStreamStatus::Disabled]);
    $session = LiveStreamSession::factory()
        ->for($liveStream)
        ->create(['status' => LiveStreamSessionStatus::Live]);
    $liveStream->forceFill(['active_session_id' => $session->id])->save();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.enable', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.show', [$organization, $liveStream]));

    $liveStream->refresh();

    expect($liveStream->status)->toBe(LiveStreamStatus::Idle)
        ->and($liveStream->active_session_id)->toBeNull()
        ->and($liveStream->restart_required)->toBeFalse();
});

test('enabling a live stream from another organization is not found', function () {
    [$user, $organization] = liveStreamAdmin();
    $otherOrganization = Organization::factory()->create();
    $otherStream = LiveStream::factory()
        ->for($otherOrganization)
        ->create(['status' => LiveStreamStatus::Disabled]);

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.enable', [$organization, $otherStream]))
        ->assertNotFound();

    expect($otherStream->refresh()->status)->toBe(LiveStreamStatus::Disabled);
});

test('operator cannot enable a disabled live stream', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $organization = Organization::factory()->create();
    $organization->users()->attach($user, ['role' => OrganizationRole::Operator->value]);
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->create(['status' => LiveStreamStatus::Disabled]);

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.enable', [$organization, $liveStream]))
        ->assertForbidden();

    expect($liveStream->refresh()->status)->toBe(LiveStreamStatus::Disabled);
});

test('enabling does not reset a stream that is already active', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create();

    $this->actingAs($user)
        ->post(route('admin.organizations.live-streams.enable', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.show', [$organization, $liveStream]));

    expect($liveStream->refresh()->status)->toBe(LiveStreamStatus::Live);
});

test('admin can delete a live stream with its sessions and viewer rollups', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->live()
        ->create();
    $session = LiveStreamSession::factory()
        ->for($liveStream)
        ->create(['status' => LiveStreamSessionStatus::Live]);
    $liveStream->forceFill(['active_session_id' => $session->id])->save();

    LiveStreamViewerRollup::factory()->create([
        'organization_id' => $organization->id,
        'live_stream_id' => $liveStream->id,
        'live_stream_session_id' => $session->id,
        'minute' => now()->startOfMinute(),
    ]);

    config([
        'services.live.control_url' => 'http://live-service.test',
        'services.live.control_token' => 'control-secret',
    ]);

    Http::fake([
        'http://live-service.test/streams/'.$liveStream->public_id.'/restart' => Http::response(['ok' => true]),
    ]);

    $this->actingAs($user)
        ->delete(route('admin.organizations.live-streams.destroy', [$organization, $liveStream]))
        ->assertRedirect(route('admin.organizations.live-streams.index', $organization));

    expect(LiveStream::query()->count())->toBe(0)
        ->and(LiveStreamSession::query()->count())->toBe(0)
        ->and(LiveStreamViewerRollup::query()->count())->toBe(0);

    Http::assertSentCount(1);
    Http::assertSent(fn (Request $request): bool => $request->method() === 'POST'
        && $request->url() === 'http://live-service.test/streams/'.$liveStream->public_id.'/restart');
});

test('deleting a live stream from another organization is not found', function () {
    [$user, $organization] = liveStreamAdmin();
    $otherOrganization = Organization::factory()->create();
    $otherStream = LiveStream::factory()->for($otherOrganization)->create();

    $this->actingAs($user)
        ->delete(route('admin.organizations.live-streams.destroy', [$organization, $otherStream]))
        ->assertNotFound();

    expect($otherStream->exists())->toBeTrue();
});

test('operator cannot delete a live stream', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $organization = Organization::factory()->create();
    $organization->users()->attach($user, ['role' => OrganizationRole::Operator->value]);
    $liveStream = LiveStream::factory()->for($organization)->create();

    $this->actingAs($user)
        ->delete(route('admin.organizations.live-streams.destroy', [$organization, $liveStream]))
        ->assertForbidden();

    expect($liveStream->exists())->toBeTrue();
});

test('restarting an idle live stream disconnects a publisher that may still be starting', function () {
    [$user, $organization] = liveStreamAdmin();
    $liveStream = LiveStream::factory()
        ->for($organization)
        ->create([
            'status' => LiveStreamStatus::Idle,
            'restart_required' => true,
        ]);

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

    expect($liveStream->refresh()->restart_required)->toBeFalse();

    Http::assertSentCount(1);
    Http::assertSent(fn (Request $request): bool => $request->method() === 'POST'
        && $request->url() === 'http://live-service.test/streams/'.$liveStream->public_id.'/restart'
        && $request->hasHeader('Authorization', 'Bearer control-secret'));
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
    Http::assertSent(fn (Request $request): bool => $request->method() === 'POST'
        && $request->url() === 'http://live-service.test/streams/'.$liveStream->public_id.'/restart'
        && $request->hasHeader('Authorization', 'Bearer control-secret'));
});
