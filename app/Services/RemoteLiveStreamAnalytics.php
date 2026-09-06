<?php

namespace App\Services;

use App\Contracts\LiveStreamAnalyticsPurger;
use App\Contracts\LiveStreamAnalyticsReader;
use App\Enums\LiveStreamViewerPeriod;
use App\Models\LiveStream;
use Illuminate\Http\Client\PendingRequest;
use Illuminate\Http\Client\Response;
use Illuminate\Support\Facades\Http;
use Throwable;

class RemoteLiveStreamAnalytics implements LiveStreamAnalyticsPurger, LiveStreamAnalyticsReader
{
    public function purge(LiveStream $liveStream): bool
    {
        $response = $this->request($liveStream, '', method: 'delete');

        return $response !== null && ($response->successful() || $response->notFound());
    }

    public function build(LiveStream $liveStream, LiveStreamViewerPeriod $period): array
    {
        $response = $this->request($liveStream, '/analytics', [
            'period' => $period->value,
        ]);

        if ($response === null || ! $response->successful()) {
            return $this->unavailable($period);
        }

        $payload = $response->json();

        if (! is_array($payload)) {
            return $this->unavailable($period);
        }

        $payload['available'] = true;
        $payload['range_label'] = $period->label();
        $payload['source_note'] = 'Analytics service data. UTC buckets; pre-cutover buckets are shown as unavailable.';
        $payload['points'] = collect($payload['points'] ?? [])
            ->map(fn (mixed $point): array => [
                'timestamp' => is_array($point) && isset($point['timestamp']) ? (string) $point['timestamp'] : '',
                'value' => is_array($point) && is_numeric($point['value'] ?? null) ? (int) $point['value'] : null,
                'complete' => is_array($point) && (bool) ($point['complete'] ?? false),
            ])
            ->values()
            ->all();

        return $payload;
    }

    public function current(LiveStream $liveStream): ?array
    {
        $response = $this->request($liveStream, '/live');

        if ($response === null || ! $response->successful()) {
            return null;
        }

        $session = $response->json('session');

        if (! is_array($session)) {
            return null;
        }

        return [
            'id' => (string) ($session['session_id'] ?? $session['id'] ?? ''),
            'status' => (string) ($session['status'] ?? 'live'),
            'settings_version' => 0,
            'hls_url' => null,
            'current_viewers' => (int) ($session['current_viewers'] ?? 0),
            'peak_viewers' => (int) ($session['peak_viewers'] ?? 0),
            'unique_viewers' => (int) ($session['unique_viewers'] ?? 0),
            'playlist_requests' => (int) ($session['playlist_requests'] ?? 0),
            'segment_requests' => (int) ($session['segment_requests'] ?? 0),
            'started_at' => $session['started_at'] ?? null,
            'ended_at' => $session['ended_at'] ?? null,
            'error_message' => null,
        ];
    }

    private function request(
        LiveStream $liveStream,
        string $path,
        array $query = [],
        string $method = 'get',
    ): ?Response {
        $baseUrl = rtrim((string) config('services.analytics.url'), '/');

        if ($baseUrl === '') {
            return null;
        }

        $url = $baseUrl
            .'/internal/v1/organizations/'.$liveStream->organization_id
            .'/streams/'.$liveStream->id.$path;

        for ($attempt = 0; $attempt < 3; $attempt++) {
            try {
                $request = $this->pendingRequest();
                $response = $method === 'delete'
                    ? $request->delete($url)
                    : $request->get($url, $query);

                if ($response->status() < 500 || $attempt === 2) {
                    return $response;
                }
            } catch (Throwable) {
                if ($attempt === 2) {
                    return null;
                }
            }

            usleep(100_000 * ($attempt + 1));
        }

        return null;
    }

    private function pendingRequest(): PendingRequest
    {
        return Http::acceptJson()
            ->withToken((string) config('services.analytics.query_token'))
            ->connectTimeout((int) config('services.analytics.connect_timeout', 2))
            ->timeout((int) config('services.analytics.timeout', 5));
    }

    /**
     * @return array<string, mixed>
     */
    private function unavailable(LiveStreamViewerPeriod $period): array
    {
        $pointCount = match ($period) {
            LiveStreamViewerPeriod::Day => 24,
            LiveStreamViewerPeriod::Month => 30,
            LiveStreamViewerPeriod::Year => 12,
        };
        $now = now()->utc();
        $start = match ($period) {
            LiveStreamViewerPeriod::Day => $now->subHours(23)->startOfHour(),
            LiveStreamViewerPeriod::Month => $now->subDays(29)->startOfDay(),
            LiveStreamViewerPeriod::Year => $now->subMonthsNoOverflow(11)->startOfMonth(),
        };

        return [
            'available' => false,
            'range_label' => $period->label(),
            'range_start' => $start->toIso8601String(),
            'range_end' => $now->toIso8601String(),
            'coverage_start' => null,
            'timezone' => 'UTC',
            'granularity' => match ($period) {
                LiveStreamViewerPeriod::Day => 'hour',
                LiveStreamViewerPeriod::Month => 'day',
                LiveStreamViewerPeriod::Year => 'month',
            },
            'source_note' => 'Analytics data is temporarily unavailable. Try again shortly.',
            'points' => array_map(
                fn (int $index): array => [
                    'timestamp' => match ($period) {
                        LiveStreamViewerPeriod::Day => $start->addHours($index)->toIso8601String(),
                        LiveStreamViewerPeriod::Month => $start->addDays($index)->toIso8601String(),
                        LiveStreamViewerPeriod::Year => $start->addMonthsNoOverflow($index)->toIso8601String(),
                    },
                    'value' => null,
                    'complete' => false,
                ],
                range(0, $pointCount - 1),
            ),
            'summary' => [
                'peak_viewers' => 0,
                'broadcasts' => 0,
                'viewer_identity_additions' => 0,
                'playback_requests' => 0,
            ],
            'generated_at' => $now->toIso8601String(),
            'data_lag_seconds' => null,
        ];
    }
}
