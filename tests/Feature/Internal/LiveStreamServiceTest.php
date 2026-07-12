<?php

use App\Enums\LiveStreamSessionStatus;
use App\Enums\LiveStreamStatus;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

beforeEach(function () {
    config(['services.live.service_token' => 'live-token']);
});

test('publish auth accepts valid stream credentials', function () {
    $liveStream = LiveStream::factory()->create(['stream_key' => 'secret-key']);

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.auth-publish'), [
            'public_id' => $liveStream->public_id,
            'stream_key' => 'secret-key',
        ])
        ->assertOk()
        ->assertJsonPath('allowed', true)
        ->assertJsonPath('stream.id', $liveStream->id);
});

test('publish auth rejects an invalid stream key', function () {
    $liveStream = LiveStream::factory()->create(['stream_key' => 'secret-key']);

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.auth-publish'), [
            'public_id' => $liveStream->public_id,
            'stream_key' => 'wrong-key',
        ])
        ->assertForbidden()
        ->assertJsonPath('allowed', false);
});

test('publish auth rejects duplicate active publisher', function () {
    $liveStream = LiveStream::factory()
        ->live()
        ->create(['stream_key' => 'secret-key']);

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.auth-publish'), [
            'public_id' => $liveStream->public_id,
            'stream_key' => 'secret-key',
        ])
        ->assertConflict()
        ->assertJsonPath('reason', 'already_live');
});

test('recover active marks stale live sessions offline', function () {
    $liveStream = LiveStream::factory()
        ->live()
        ->create();

    $session = LiveStreamSession::factory()
        ->for($liveStream)
        ->create();

    $liveStream->forceFill(['active_session_id' => $session->id])->save();

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.recover-active'))
        ->assertOk()
        ->assertJsonPath('recovered', 1);

    $liveStream->refresh();
    $session->refresh();

    expect($liveStream->status)->toBe(LiveStreamStatus::Offline)
        ->and($liveStream->active_session_id)->toBeNull()
        ->and($session->status)->toBe(LiveStreamSessionStatus::Failed)
        ->and($session->error_message)->toBe('Live service restarted.')
        ->and($session->ended_at)->not->toBeNull();
});

test('service callbacks track live session and viewer rollups', function () {
    $liveStream = LiveStream::factory()->create([
        'stream_key' => 'secret-key',
        'settings_version' => 5,
    ]);

    $startResponse = $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), [
            'public_id' => $liveStream->public_id,
            'external_id' => 'go-session-1',
            'hls_url' => 'https://stream.example/live/'.$liveStream->public_id.'/index.m3u8',
            'hls_prefix' => 'live/'.$liveStream->public_id,
        ])
        ->assertOk();

    $sessionId = $startResponse->json('session_id');

    $liveStream->refresh();
    expect($liveStream->status)->toBe(LiveStreamStatus::Live)
        ->and($liveStream->active_session_id)->toBe($sessionId);

    $session = LiveStreamSession::query()->findOrFail($sessionId);
    expect($session->status)->toBe(LiveStreamSessionStatus::Live)
        ->and($session->settings_version)->toBe(5);

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.viewer-snapshot'), [
            'public_id' => $liveStream->public_id,
            'session_id' => $sessionId,
            'minute' => '2026-05-22T18:15:00Z',
            'current_viewers' => 12,
            'unique_viewers_seen' => 18,
            'playlist_requests' => 44,
            'segment_requests' => 91,
        ])
        ->assertOk();

    $session->refresh();
    expect($session->current_viewers)->toBe(12)
        ->and($session->peak_viewers)->toBe(12)
        ->and($session->unique_viewers)->toBe(18)
        ->and($session->playlist_requests)->toBe(44)
        ->and($session->segment_requests)->toBe(91);

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-ended'), [
            'public_id' => $liveStream->public_id,
            'session_id' => $sessionId,
            'peak_viewers' => 20,
            'unique_viewers' => 33,
            'playlist_requests' => 100,
            'segment_requests' => 250,
        ])
        ->assertOk();

    $liveStream->refresh();
    $session->refresh();

    expect($liveStream->status)->toBe(LiveStreamStatus::Offline)
        ->and($liveStream->active_session_id)->toBeNull()
        ->and($session->status)->toBe(LiveStreamSessionStatus::Ended)
        ->and($session->peak_viewers)->toBe(20)
        ->and($session->unique_viewers)->toBe(33);

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.viewer-snapshot'), [
            'public_id' => $liveStream->public_id,
            'session_id' => $sessionId,
            'current_viewers' => 99,
            'unique_viewers_seen' => 99,
            'playlist_requests' => 999,
            'segment_requests' => 999,
        ])
        ->assertOk();

    expect($session->fresh()->current_viewers)->toBe(0)
        ->and($session->playlist_requests)->toBe(100);
});

test('session start callbacks are idempotent by external id', function () {
    $liveStream = LiveStream::factory()->create();
    $payload = [
        'public_id' => $liveStream->public_id,
        'external_id' => 'go-session-idempotent',
        'hls_prefix' => 'live/'.$liveStream->public_id,
    ];

    $firstSessionId = $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), $payload)
        ->assertOk()
        ->json('session_id');

    $secondSessionId = $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), $payload)
        ->assertOk()
        ->json('session_id');

    expect($secondSessionId)->toBe($firstSessionId)
        ->and($liveStream->sessions()->count())->toBe(1)
        ->and($liveStream->fresh()->active_session_id)->toBe($firstSessionId);
});

test('late session start retries return the original completed session', function () {
    $liveStream = LiveStream::factory()->create();
    $payload = [
        'public_id' => $liveStream->public_id,
        'external_id' => 'go-session-completed',
    ];

    $sessionId = $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), $payload)
        ->assertOk()
        ->json('session_id');

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-ended'), [
            'public_id' => $liveStream->public_id,
            'session_id' => $sessionId,
        ])
        ->assertOk();

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), $payload)
        ->assertOk()
        ->assertJsonPath('session_id', $sessionId);

    expect($liveStream->sessions()->count())->toBe(1)
        ->and($liveStream->fresh()->status)->toBe(LiveStreamStatus::Offline)
        ->and($liveStream->active_session_id)->toBeNull()
        ->and(LiveStreamSession::query()->findOrFail($sessionId)->status)->toBe(LiveStreamSessionStatus::Ended);
});

test('session start rejects a different external id while a stream is active', function () {
    $liveStream = LiveStream::factory()->create();

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), [
            'public_id' => $liveStream->public_id,
            'external_id' => 'go-session-first',
        ])
        ->assertOk();

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), [
            'public_id' => $liveStream->public_id,
            'external_id' => 'go-session-second',
        ])
        ->assertConflict();

    expect($liveStream->sessions()->count())->toBe(1);
});

test('session start requires an external id', function () {
    $liveStream = LiveStream::factory()->create();

    $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), [
            'public_id' => $liveStream->public_id,
        ])
        ->assertUnprocessable()
        ->assertInvalid('external_id');
});

test('external session ids are unique within each live stream', function () {
    $firstStream = LiveStream::factory()->create();
    $secondStream = LiveStream::factory()->create();

    $firstSessionId = $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), [
            'public_id' => $firstStream->public_id,
            'external_id' => 'shared-worker-session-id',
        ])
        ->assertOk()
        ->json('session_id');

    $secondSessionId = $this->withHeader('X-Live-Service-Token', 'live-token')
        ->postJson(route('internal.live.session-started'), [
            'public_id' => $secondStream->public_id,
            'external_id' => 'shared-worker-session-id',
        ])
        ->assertOk()
        ->json('session_id');

    expect($secondSessionId)->not->toBe($firstSessionId)
        ->and(LiveStreamSession::query()->where('external_id', 'shared-worker-session-id')->count())->toBe(2);
});
