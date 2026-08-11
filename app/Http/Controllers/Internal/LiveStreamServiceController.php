<?php

namespace App\Http\Controllers\Internal;

use App\Enums\LiveStreamSessionStatus;
use App\Enums\LiveStreamStatus;
use App\Http\Controllers\Controller;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerRollup;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Carbon;
use Illuminate\Support\Facades\DB;

class LiveStreamServiceController extends Controller
{
    public function authPublish(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $validated = $request->validate([
            'public_id' => ['required', 'string', 'max:255'],
            'stream_key' => ['required', 'string', 'max:255'],
        ]);

        $liveStream = LiveStream::query()
            ->with('profile:id,organization_id,qualities')
            ->where('public_id', $validated['public_id'])
            ->first();

        if (! $liveStream instanceof LiveStream) {
            return response()->json(['allowed' => false, 'reason' => 'not_found'], 404);
        }

        if ($liveStream->status === LiveStreamStatus::Disabled) {
            return response()->json(['allowed' => false, 'reason' => 'disabled'], 403);
        }

        if (! hash_equals($liveStream->stream_key, $validated['stream_key'])) {
            return response()->json(['allowed' => false, 'reason' => 'invalid_key'], 403);
        }

        if ($liveStream->status === LiveStreamStatus::Live) {
            return response()->json(['allowed' => false, 'reason' => 'already_live'], 409);
        }

        return response()->json([
            'allowed' => true,
            'stream' => [
                'id' => $liveStream->id,
                'organization_id' => $liveStream->organization_id,
                'public_id' => $liveStream->public_id,
                'settings_version' => $liveStream->settings_version,
                'hls_url' => $liveStream->hls_url,
                'qualities' => $liveStream->profile?->qualities ?? [],
            ],
        ]);
    }

    public function sessionStarted(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $validated = $request->validate([
            'public_id' => ['required', 'string', 'max:255'],
            'external_id' => ['required', 'string', 'max:255'],
            'hls_url' => ['nullable', 'string', 'max:2048'],
            'hls_prefix' => ['nullable', 'string', 'max:1024'],
        ]);

        $liveStream = $this->findStream($validated['public_id']);

        $session = DB::transaction(function () use ($liveStream, $validated): LiveStreamSession {
            // Lock the row so two near-simultaneous publishers cannot both create
            // a session and clobber active_session_id.
            $locked = LiveStream::query()
                ->whereKey($liveStream->getKey())
                ->lockForUpdate()
                ->firstOrFail();

            $existingSession = $locked->sessions()
                ->where('external_id', $validated['external_id'])
                ->first();

            if ($existingSession instanceof LiveStreamSession) {
                return $existingSession;
            }

            abort_if($locked->status === LiveStreamStatus::Disabled, 403, 'Live stream is disabled.');
            abort_unless($locked->active_session_id === null, 409, 'Live stream already has an active session.');

            $session = $locked->sessions()->create([
                'external_id' => $validated['external_id'],
                'status' => LiveStreamSessionStatus::Live,
                'settings_version' => $locked->settings_version,
                'hls_url' => $validated['hls_url'] ?? $locked->hls_url,
                'hls_prefix' => $validated['hls_prefix'] ?? null,
                'started_at' => now(),
            ]);

            $locked->forceFill([
                'status' => LiveStreamStatus::Live,
                'active_session_id' => $session->id,
                'last_started_at' => $session->started_at,
            ])->save();

            return $session;
        });

        return response()->json([
            'ok' => true,
            'session_id' => $session->id,
        ]);
    }

    public function sessionEnded(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $validated = $request->validate([
            'public_id' => ['required', 'string', 'max:255'],
            'session_id' => ['required', 'uuid'],
            'minute' => ['required_with:current_viewers', 'date'],
            'current_viewers' => ['required_with:minute', 'integer', 'min:0'],
            'peak_viewers' => ['nullable', 'integer', 'min:0'],
            'unique_viewers' => ['nullable', 'integer', 'min:0'],
            'playlist_requests' => ['nullable', 'integer', 'min:0'],
            'segment_requests' => ['nullable', 'integer', 'min:0'],
        ]);

        $liveStream = $this->findStream($validated['public_id']);
        $session = $this->findSession($liveStream, $validated['session_id']);
        $minute = isset($validated['minute'])
            ? Carbon::parse($validated['minute'])->utc()->startOfMinute()
            : null;

        DB::transaction(function () use ($liveStream, $session, $validated, $minute): void {
            [$lockedLiveStream, $lockedSession] = $this->lockStreamAndSession($liveStream, $session);

            if ($lockedSession->status === LiveStreamSessionStatus::Ended) {
                return;
            }

            if (! filled(config('services.analytics.url')) && $minute !== null && array_key_exists('current_viewers', $validated)) {
                $this->recordViewerSample(
                    $lockedLiveStream,
                    $lockedSession,
                    $minute,
                    (int) $validated['current_viewers'],
                    (int) ($validated['unique_viewers'] ?? $lockedSession->unique_viewers),
                    (int) ($validated['playlist_requests'] ?? $lockedSession->playlist_requests),
                    (int) ($validated['segment_requests'] ?? $lockedSession->segment_requests),
                    (int) ($validated['peak_viewers'] ?? $validated['current_viewers']),
                );
            }

            $lockedSession->forceFill([
                'status' => LiveStreamSessionStatus::Ended,
                'peak_viewers' => max($lockedSession->peak_viewers, (int) ($validated['peak_viewers'] ?? 0)),
                'unique_viewers' => max($lockedSession->unique_viewers, (int) ($validated['unique_viewers'] ?? 0)),
                'playlist_requests' => max($lockedSession->playlist_requests, (int) ($validated['playlist_requests'] ?? 0)),
                'segment_requests' => max($lockedSession->segment_requests, (int) ($validated['segment_requests'] ?? 0)),
                'current_viewers' => 0,
                'ended_at' => $lockedSession->ended_at ?? now(),
            ])->save();

            $this->closeLockedStreamIfActive($lockedLiveStream, $lockedSession, LiveStreamStatus::Offline);
        });

        return response()->json(['ok' => true]);
    }

    public function sessionFailed(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $validated = $request->validate([
            'public_id' => ['required', 'string', 'max:255'],
            'session_id' => ['required', 'uuid'],
            'error_message' => ['nullable', 'string', 'max:2000'],
        ]);

        $liveStream = $this->findStream($validated['public_id']);
        $session = $this->findSession($liveStream, $validated['session_id']);

        DB::transaction(function () use ($liveStream, $session, $validated): void {
            [$lockedLiveStream, $lockedSession] = $this->lockStreamAndSession($liveStream, $session);

            $lockedSession->forceFill([
                'status' => LiveStreamSessionStatus::Failed,
                'error_message' => $validated['error_message'] ?? null,
                'current_viewers' => 0,
                'ended_at' => $lockedSession->ended_at ?? now(),
            ])->save();

            $this->closeLockedStreamIfActive($lockedLiveStream, $lockedSession, LiveStreamStatus::Failed);
        });

        return response()->json(['ok' => true]);
    }

    public function recoverActive(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $recovered = DB::transaction(function (): int {
            // No whereNotNull filter: a stream stuck Live/Restarting with a null
            // active_session_id must also be forced Offline, otherwise authPublish
            // keeps rejecting it as already-live forever.
            $liveStreams = LiveStream::query()
                ->whereIn('status', [LiveStreamStatus::Live, LiveStreamStatus::Restarting])
                ->lockForUpdate()
                ->get();

            foreach ($liveStreams as $liveStream) {
                $session = $liveStream->sessions()
                    ->whereKey($liveStream->active_session_id)
                    ->first();

                if ($session instanceof LiveStreamSession) {
                    $session->forceFill([
                        'status' => LiveStreamSessionStatus::Failed,
                        'error_message' => 'Live service restarted.',
                        'current_viewers' => 0,
                        'ended_at' => now(),
                    ])->save();
                }

                $liveStream->forceFill([
                    'status' => LiveStreamStatus::Offline,
                    'active_session_id' => null,
                    'last_ended_at' => $session?->ended_at ?? now(),
                ])->save();
            }

            return $liveStreams->count();
        });

        return response()->json([
            'ok' => true,
            'recovered' => $recovered,
        ]);
    }

    public function viewerSnapshot(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        if (filled(config('services.analytics.url'))) {
            // Viewer metrics are owned by the isolated analytics service after
            // cutover. Keep this callback as a compatibility no-op so an older
            // golang-live binary can be upgraded without a callback failure.
            return response()->json(['ok' => true, 'analytics' => 'remote']);
        }

        $validated = $request->validate([
            'public_id' => ['required', 'string', 'max:255'],
            'session_id' => ['required', 'uuid'],
            'minute' => ['nullable', 'date'],
            'current_viewers' => ['required', 'integer', 'min:0'],
            'unique_viewers_seen' => ['required', 'integer', 'min:0'],
            'playlist_requests' => ['required', 'integer', 'min:0'],
            'segment_requests' => ['required', 'integer', 'min:0'],
        ]);

        $liveStream = $this->findStream($validated['public_id']);
        $session = $this->findSession($liveStream, $validated['session_id']);

        $minute = isset($validated['minute'])
            ? Carbon::parse($validated['minute'])->utc()->startOfMinute()
            : now()->utc()->startOfMinute();

        DB::transaction(function () use ($liveStream, $session, $validated, $minute): void {
            [$lockedLiveStream, $lockedSession] = $this->lockStreamAndSession($liveStream, $session);

            if (in_array($lockedSession->status, [LiveStreamSessionStatus::Ended, LiveStreamSessionStatus::Failed], true)) {
                return;
            }

            $this->recordViewerSample(
                $lockedLiveStream,
                $lockedSession,
                $minute,
                (int) $validated['current_viewers'],
                (int) $validated['unique_viewers_seen'],
                (int) $validated['playlist_requests'],
                (int) $validated['segment_requests'],
            );
        });

        return response()->json(['ok' => true]);
    }

    private function recordViewerSample(
        LiveStream $liveStream,
        LiveStreamSession $session,
        Carbon $minute,
        int $currentViewers,
        int $uniqueViewers,
        int $playlistRequests,
        int $segmentRequests,
        ?int $samplePeakViewers = null,
    ): void {
        $uniqueViewers = max($session->unique_viewers, $uniqueViewers);
        $playlistRequests = max($session->playlist_requests, $playlistRequests);
        $segmentRequests = max($session->segment_requests, $segmentRequests);

        $rollup = LiveStreamViewerRollup::query()->firstOrNew([
            'live_stream_session_id' => $session->id,
            'minute' => $minute,
        ]);

        $rollup->forceFill([
            'organization_id' => $liveStream->organization_id,
            'live_stream_id' => $liveStream->id,
            'current_viewers' => $currentViewers,
            'peak_viewers' => max(
                (int) $rollup->peak_viewers,
                (int) $rollup->current_viewers,
                $currentViewers,
                $samplePeakViewers ?? $currentViewers,
            ),
            'unique_viewers_seen' => max($rollup->unique_viewers_seen, $uniqueViewers),
            'playlist_requests' => max($rollup->playlist_requests, $playlistRequests),
            'segment_requests' => max($rollup->segment_requests, $segmentRequests),
            'viewer_identity_additions' => (int) $rollup->viewer_identity_additions + ($uniqueViewers - $session->unique_viewers),
            'playlist_requests_delta' => (int) $rollup->playlist_requests_delta + ($playlistRequests - $session->playlist_requests),
            'segment_requests_delta' => (int) $rollup->segment_requests_delta + ($segmentRequests - $session->segment_requests),
            'sample_count' => (int) $rollup->sample_count + 1,
        ])->save();

        $session->forceFill([
            'current_viewers' => $currentViewers,
            'peak_viewers' => max($session->peak_viewers, $currentViewers),
            'unique_viewers' => $uniqueViewers,
            'playlist_requests' => $playlistRequests,
            'segment_requests' => $segmentRequests,
        ])->save();
    }

    private function authorizeService(Request $request): void
    {
        $expected = config('services.live.service_token');

        abort_unless(is_string($expected) && $expected !== '', 503, 'Live service token is not configured.');

        $provided = $request->header('X-Live-Service-Token', '');

        abort_unless(hash_equals($expected, $provided), 403);
    }

    /**
     * Only flip the stream's lifecycle state when the reporting session is still
     * the active one. A late callback for a superseded session must update its own
     * row but must not knock a newer, currently-live session offline.
     */
    private function closeLockedStreamIfActive(
        LiveStream $liveStream,
        LiveStreamSession $session,
        LiveStreamStatus $status,
    ): void {
        if ($liveStream->active_session_id !== $session->id) {
            return;
        }

        $liveStream->forceFill([
            'status' => $status,
            'active_session_id' => null,
            'last_ended_at' => $session->ended_at,
        ])->save();
    }

    /**
     * @return array{LiveStream, LiveStreamSession}
     */
    private function lockStreamAndSession(
        LiveStream $liveStream,
        LiveStreamSession $session,
    ): array {
        $lockedLiveStream = LiveStream::query()
            ->whereKey($liveStream->id)
            ->lockForUpdate()
            ->firstOrFail();

        $lockedSession = $lockedLiveStream->sessions()
            ->whereKey($session->id)
            ->lockForUpdate()
            ->firstOrFail();

        return [$lockedLiveStream, $lockedSession];
    }

    private function findStream(string $publicId): LiveStream
    {
        return LiveStream::query()
            ->where('public_id', $publicId)
            ->firstOrFail();
    }

    private function findSession(LiveStream $liveStream, string $sessionId): LiveStreamSession
    {
        return $liveStream->sessions()->findOrFail($sessionId);
    }
}
