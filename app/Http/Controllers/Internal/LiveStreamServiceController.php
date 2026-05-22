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
                'recording_enabled' => $liveStream->recording_enabled,
                'hls_url' => $liveStream->hls_url,
            ],
        ]);
    }

    public function sessionStarted(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $validated = $request->validate([
            'public_id' => ['required', 'string', 'max:255'],
            'external_id' => ['nullable', 'string', 'max:255'],
            'hls_url' => ['nullable', 'string', 'max:2048'],
            'hls_prefix' => ['nullable', 'string', 'max:1024'],
        ]);

        $liveStream = $this->findStream($validated['public_id']);

        $session = DB::transaction(function () use ($liveStream, $validated): LiveStreamSession {
            $session = $liveStream->sessions()->create([
                'external_id' => $validated['external_id'] ?? null,
                'status' => LiveStreamSessionStatus::Live,
                'settings_version' => $liveStream->settings_version,
                'recording_enabled' => $liveStream->recording_enabled,
                'hls_url' => $validated['hls_url'] ?? $liveStream->hls_url,
                'hls_prefix' => $validated['hls_prefix'] ?? null,
                'started_at' => now(),
            ]);

            $liveStream->forceFill([
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
            'recording_path' => ['nullable', 'string', 'max:2048'],
            'peak_viewers' => ['nullable', 'integer', 'min:0'],
            'unique_viewers' => ['nullable', 'integer', 'min:0'],
            'playlist_requests' => ['nullable', 'integer', 'min:0'],
            'segment_requests' => ['nullable', 'integer', 'min:0'],
        ]);

        $liveStream = $this->findStream($validated['public_id']);
        $session = $this->findSession($liveStream, $validated['session_id']);

        DB::transaction(function () use ($liveStream, $session, $validated): void {
            $session->forceFill([
                'status' => LiveStreamSessionStatus::Ended,
                'recording_path' => $validated['recording_path'] ?? $session->recording_path,
                'peak_viewers' => max($session->peak_viewers, (int) ($validated['peak_viewers'] ?? 0)),
                'unique_viewers' => max($session->unique_viewers, (int) ($validated['unique_viewers'] ?? 0)),
                'playlist_requests' => max($session->playlist_requests, (int) ($validated['playlist_requests'] ?? 0)),
                'segment_requests' => max($session->segment_requests, (int) ($validated['segment_requests'] ?? 0)),
                'current_viewers' => 0,
                'ended_at' => now(),
            ])->save();

            $liveStream->forceFill([
                'status' => LiveStreamStatus::Offline,
                'active_session_id' => null,
                'last_ended_at' => $session->ended_at,
            ])->save();
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
            $session->forceFill([
                'status' => LiveStreamSessionStatus::Failed,
                'error_message' => $validated['error_message'] ?? null,
                'current_viewers' => 0,
                'ended_at' => now(),
            ])->save();

            $liveStream->forceFill([
                'status' => LiveStreamStatus::Failed,
                'active_session_id' => null,
                'last_ended_at' => $session->ended_at,
            ])->save();
        });

        return response()->json(['ok' => true]);
    }

    public function recoverActive(Request $request): JsonResponse
    {
        $this->authorizeService($request);

        $recovered = DB::transaction(function (): int {
            $liveStreams = LiveStream::query()
                ->whereIn('status', [LiveStreamStatus::Live, LiveStreamStatus::Restarting])
                ->whereNotNull('active_session_id')
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
            ? Carbon::parse($validated['minute'])->startOfMinute()
            : now()->startOfMinute();

        DB::transaction(function () use ($liveStream, $session, $validated, $minute): void {
            LiveStreamViewerRollup::query()->updateOrCreate(
                [
                    'live_stream_session_id' => $session->id,
                    'minute' => $minute,
                ],
                [
                    'organization_id' => $liveStream->organization_id,
                    'live_stream_id' => $liveStream->id,
                    'current_viewers' => $validated['current_viewers'],
                    'unique_viewers_seen' => $validated['unique_viewers_seen'],
                    'playlist_requests' => $validated['playlist_requests'],
                    'segment_requests' => $validated['segment_requests'],
                ],
            );

            $session->forceFill([
                'current_viewers' => $validated['current_viewers'],
                'peak_viewers' => max($session->peak_viewers, (int) $validated['current_viewers']),
                'unique_viewers' => max($session->unique_viewers, (int) $validated['unique_viewers_seen']),
                'playlist_requests' => max($session->playlist_requests, (int) $validated['playlist_requests']),
                'segment_requests' => max($session->segment_requests, (int) $validated['segment_requests']),
            ])->save();
        });

        return response()->json(['ok' => true]);
    }

    private function authorizeService(Request $request): void
    {
        $expected = config('services.live.service_token');

        abort_unless(is_string($expected) && $expected !== '', 503, 'Live service token is not configured.');

        $provided = $request->header('X-Live-Service-Token', '');

        abort_unless(hash_equals($expected, $provided), 403);
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
