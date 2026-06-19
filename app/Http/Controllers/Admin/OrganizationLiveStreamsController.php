<?php

namespace App\Http\Controllers\Admin;

use App\Enums\LiveStreamStatus;
use App\Http\Controllers\Controller;
use App\Http\Requests\Admin\StoreLiveStreamRequest;
use App\Http\Requests\Admin\UpdateLiveStreamSettingsRequest;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\Organization;
use App\Services\LiveStreamControlClient;
use App\Services\LiveStreamEndpointService;
use Illuminate\Http\RedirectResponse;
use Illuminate\Support\Collection;
use Inertia\Inertia;
use Inertia\Response;

class OrganizationLiveStreamsController extends Controller
{
    public function index(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        $liveStreams = $organization->liveStreams()
            ->withMax('sessions as latest_session_started_at', 'started_at')
            ->orderByDesc('created_at')
            ->get();

        $latestSessions = $this->latestSessionsFor($liveStreams);

        return Inertia::render('admin/live-streams/index', [
            'organization' => $this->organizationPayload($organization),
            'liveStreams' => $liveStreams
                ->map(fn (LiveStream $liveStream): array => [
                    'id' => $liveStream->id,
                    'title' => $liveStream->title,
                    'public_id' => $liveStream->public_id,
                    'status' => $liveStream->status->value,
                    'status_label' => $liveStream->status->label(),
                    'recording_enabled' => $liveStream->recording_enabled,
                    'restart_required' => $liveStream->restart_required,
                    'current_viewers' => $latestSessions->get($liveStream->id)?->current_viewers ?? 0,
                    'peak_viewers' => $latestSessions->get($liveStream->id)?->peak_viewers ?? 0,
                    'created_at' => $liveStream->created_at?->toIso8601String(),
                    'last_started_at' => $liveStream->last_started_at?->toIso8601String(),
                ])
                ->all(),
        ]);
    }

    public function create(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        return Inertia::render('admin/live-streams/create', [
            'organization' => $this->organizationPayload($organization),
        ]);
    }

    public function store(
        StoreLiveStreamRequest $request,
        Organization $organization,
        LiveStreamEndpointService $endpoints,
    ): RedirectResponse {
        $this->authorize('manage', $organization);

        $publicId = $endpoints->generatePublicId();

        $liveStream = $organization->liveStreams()->create([
            'created_by_id' => $request->user()?->getKey(),
            'title' => $request->string('title'),
            'public_id' => $publicId,
            'stream_key' => $endpoints->generateStreamKey(),
            'status' => LiveStreamStatus::Idle,
            'recording_enabled' => $request->boolean('recording_enabled'),
            'restart_required' => false,
            'settings_version' => 1,
            'rtmp_url' => $endpoints->rtmpUrl(),
            'hls_url' => $endpoints->hlsUrl($publicId),
        ]);

        return to_route('admin.organizations.live-streams.show', [$organization, $liveStream])
            ->with('toast', ['type' => 'success', 'message' => __('Live stream created.')]);
    }

    public function show(Organization $organization, LiveStream $liveStream): Response
    {
        $this->authorize('manage', $organization);
        abort_unless($liveStream->organization_id === $organization->id, 404);

        $currentSession = $liveStream->sessions()
            ->whereKey($liveStream->active_session_id)
            ->first();

        $recentSessions = $liveStream->sessions()
            ->latest('started_at')
            ->limit(10)
            ->get()
            ->map(fn (LiveStreamSession $session): array => $this->sessionPayload($session))
            ->all();

        $rollupSessionId = $currentSession?->id
            ?? $liveStream->sessions()->latest('started_at')->value('id');

        $rollups = $liveStream->viewerRollups()
            ->when($rollupSessionId, fn ($query) => $query->where('live_stream_session_id', $rollupSessionId))
            ->latest('minute')
            ->limit(60)
            ->get()
            ->sortBy('minute')
            ->values()
            ->map(fn ($rollup): array => [
                'minute' => $rollup->minute?->toIso8601String(),
                'current_viewers' => $rollup->current_viewers,
                'unique_viewers_seen' => $rollup->unique_viewers_seen,
                'playlist_requests' => $rollup->playlist_requests,
                'segment_requests' => $rollup->segment_requests,
            ])
            ->all();

        return Inertia::render('admin/live-streams/show', [
            'organization' => $this->organizationPayload($organization),
            'liveStream' => [
                'id' => $liveStream->id,
                'title' => $liveStream->title,
                'public_id' => $liveStream->public_id,
                'stream_key' => $liveStream->stream_key,
                'stream_path' => $liveStream->public_id,
                'status' => $liveStream->status->value,
                'status_label' => $liveStream->status->label(),
                'recording_enabled' => $liveStream->recording_enabled,
                'restart_required' => $liveStream->restart_required,
                'settings_version' => $liveStream->settings_version,
                'rtmp_url' => $liveStream->rtmp_url,
                'hls_url' => $liveStream->hls_url,
                'last_started_at' => $liveStream->last_started_at?->toIso8601String(),
                'last_ended_at' => $liveStream->last_ended_at?->toIso8601String(),
                'current_session' => $currentSession ? $this->sessionPayload($currentSession) : null,
                'recent_sessions' => $recentSessions,
                'viewer_rollups' => $rollups,
            ],
        ]);
    }

    public function update(
        UpdateLiveStreamSettingsRequest $request,
        Organization $organization,
        LiveStream $liveStream,
    ): RedirectResponse {
        $this->authorize('manage', $organization);
        abort_unless($liveStream->organization_id === $organization->id, 404);

        $recordingEnabled = $request->boolean('recording_enabled');
        $requiresRestart = $liveStream->recording_enabled !== $recordingEnabled;

        $liveStream->fill([
            'title' => $request->string('title'),
            'recording_enabled' => $recordingEnabled,
        ]);

        if ($requiresRestart) {
            $liveStream->settings_version++;

            if ($liveStream->isLive()) {
                $liveStream->restart_required = true;
            }
        }

        $liveStream->save();

        return to_route('admin.organizations.live-streams.show', [$organization, $liveStream])
            ->with('toast', ['type' => 'success', 'message' => __('Live stream settings updated.')]);
    }

    public function rotateKey(
        Organization $organization,
        LiveStream $liveStream,
        LiveStreamEndpointService $endpoints,
        LiveStreamControlClient $client,
    ): RedirectResponse {
        $this->authorize('manage', $organization);
        abort_unless($liveStream->organization_id === $organization->id, 404);

        $wasLive = $liveStream->isLive();

        $liveStream->forceFill([
            'stream_key' => $endpoints->generateStreamKey(),
            'settings_version' => $liveStream->settings_version + 1,
            'restart_required' => $wasLive || $liveStream->restart_required,
        ])->save();

        // Kick the active publisher so the rotated (old) key stops working now,
        // rather than remaining valid until the streamer voluntarily reconnects.
        if ($wasLive) {
            $client->restart($liveStream);
        }

        return to_route('admin.organizations.live-streams.show', [$organization, $liveStream])
            ->with('toast', ['type' => 'success', 'message' => __('Stream key rotated.')]);
    }

    public function restart(
        Organization $organization,
        LiveStream $liveStream,
        LiveStreamControlClient $client,
    ): RedirectResponse {
        $this->authorize('manage', $organization);
        abort_unless($liveStream->organization_id === $organization->id, 404);

        if (! $liveStream->isLive()) {
            $liveStream->forceFill(['restart_required' => false])->save();

            return to_route('admin.organizations.live-streams.show', [$organization, $liveStream])
                ->with('toast', ['type' => 'success', 'message' => __('Settings will apply on the next stream start.')]);
        }

        if (! $client->restart($liveStream)) {
            return to_route('admin.organizations.live-streams.show', [$organization, $liveStream])
                ->with('toast', ['type' => 'error', 'message' => __('Live service did not accept the restart request.')]);
        }

        $liveStream->forceFill([
            'status' => LiveStreamStatus::Restarting,
            'restart_required' => false,
        ])->save();

        return to_route('admin.organizations.live-streams.show', [$organization, $liveStream])
            ->with('toast', ['type' => 'success', 'message' => __('Restart requested.')]);
    }

    public function disable(Organization $organization, LiveStream $liveStream): RedirectResponse
    {
        $this->authorize('manage', $organization);
        abort_unless($liveStream->organization_id === $organization->id, 404);

        $liveStream->forceFill([
            'status' => LiveStreamStatus::Disabled,
            'restart_required' => false,
        ])->save();

        return to_route('admin.organizations.live-streams.index', $organization)
            ->with('toast', ['type' => 'success', 'message' => __('Live stream disabled.')]);
    }

    /**
     * @return array{id: string, name: string}
     */
    private function organizationPayload(Organization $organization): array
    {
        return [
            'id' => $organization->id,
            'name' => $organization->name,
        ];
    }

    /**
     * @param  Collection<int, LiveStream>  $liveStreams
     * @return Collection<string, LiveStreamSession>
     */
    private function latestSessionsFor(Collection $liveStreams): Collection
    {
        $startedAtByStream = $liveStreams
            ->filter(fn (LiveStream $liveStream): bool => $liveStream->latest_session_started_at !== null)
            ->mapWithKeys(fn (LiveStream $liveStream): array => [
                $liveStream->id => $liveStream->latest_session_started_at,
            ]);

        if ($startedAtByStream->isEmpty()) {
            return collect();
        }

        return LiveStreamSession::query()
            ->whereIn('live_stream_id', $startedAtByStream->keys())
            ->whereIn('started_at', $startedAtByStream->values())
            ->orderByDesc('created_at')
            ->get()
            ->unique('live_stream_id')
            ->keyBy('live_stream_id');
    }

    /**
     * @return array<string, mixed>
     */
    private function sessionPayload(LiveStreamSession $session): array
    {
        return [
            'id' => $session->id,
            'status' => $session->status->value,
            'settings_version' => $session->settings_version,
            'recording_enabled' => $session->recording_enabled,
            'hls_url' => $session->hls_url,
            'recording_path' => $session->recording_path,
            'current_viewers' => $session->current_viewers,
            'peak_viewers' => $session->peak_viewers,
            'unique_viewers' => $session->unique_viewers,
            'playlist_requests' => $session->playlist_requests,
            'segment_requests' => $session->segment_requests,
            'started_at' => $session->started_at?->toIso8601String(),
            'ended_at' => $session->ended_at?->toIso8601String(),
            'error_message' => $session->error_message,
        ];
    }
}
