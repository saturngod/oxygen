<?php

namespace App\Services;

use App\Enums\LiveStreamViewerPeriod;
use App\Models\LiveStream;
use App\Models\LiveStreamViewerHourlyRollup;
use App\Models\LiveStreamViewerRollup;
use Carbon\CarbonImmutable;
use Illuminate\Support\Facades\Date;

class LiveStreamViewerAnalytics
{
    /**
     * @return array{
     *     range_label: string,
     *     range_start: string,
     *     range_end: string,
     *     timezone: string,
     *     granularity: string,
     *     source_note: string,
     *     points: list<array{timestamp: string, value: int}>,
     *     summary: array{peak_viewers: int, broadcasts: int, viewer_identity_additions: int, playback_requests: int}
     * }
     */
    public function build(LiveStream $liveStream, LiveStreamViewerPeriod $period): array
    {
        $end = Date::now()->toImmutable()->utc();
        $start = $this->rangeStart($period, $end);
        $points = $this->emptyPoints($period, $start);
        $hourlyMetrics = $this->hourlyMetrics($liveStream, $start, $end);

        foreach ($hourlyMetrics as $metrics) {
            $key = $this->bucketKey($period, $metrics['bucket_start']);

            if (isset($points[$key])) {
                $points[$key]['value'] = max($points[$key]['value'], $metrics['peak_viewers']);
            }
        }

        return [
            'range_label' => $period->label(),
            'range_start' => $start->toIso8601String(),
            'range_end' => $end->toIso8601String(),
            'timezone' => 'UTC',
            'granularity' => match ($period) {
                LiveStreamViewerPeriod::Day => 'hour',
                LiveStreamViewerPeriod::Month => 'day',
                LiveStreamViewerPeriod::Year => 'month',
            },
            'source_note' => 'Recent data uses minute samples. Completed UTC hours are retained as durable aggregates.',
            'points' => array_values($points),
            'summary' => [
                'peak_viewers' => (int) (collect($points)->max('value') ?? 0),
                'broadcasts' => $this->overlappingBroadcastCount($liveStream, $start, $end),
                'viewer_identity_additions' => (int) collect($hourlyMetrics)->sum('viewer_identity_additions'),
                'playback_requests' => (int) collect($hourlyMetrics)->sum(
                    fn (array $metrics): int => $metrics['playlist_requests'] + $metrics['segment_requests'],
                ),
            ],
        ];
    }

    private function rangeStart(LiveStreamViewerPeriod $period, CarbonImmutable $end): CarbonImmutable
    {
        return match ($period) {
            LiveStreamViewerPeriod::Day => $end->subHours(23)->startOfHour(),
            LiveStreamViewerPeriod::Month => $end->subDays(29)->startOfDay(),
            LiveStreamViewerPeriod::Year => $end->subMonthsNoOverflow(11)->startOfMonth(),
        };
    }

    /**
     * @return array<string, array{timestamp: string, value: int}>
     */
    private function emptyPoints(LiveStreamViewerPeriod $period, CarbonImmutable $start): array
    {
        $count = match ($period) {
            LiveStreamViewerPeriod::Day => 24,
            LiveStreamViewerPeriod::Month => 30,
            LiveStreamViewerPeriod::Year => 12,
        };
        $points = [];

        for ($index = 0; $index < $count; $index++) {
            $timestamp = match ($period) {
                LiveStreamViewerPeriod::Day => $start->addHours($index),
                LiveStreamViewerPeriod::Month => $start->addDays($index),
                LiveStreamViewerPeriod::Year => $start->addMonthsNoOverflow($index),
            };

            $points[$this->bucketKey($period, $timestamp)] = [
                'timestamp' => $timestamp->toIso8601String(),
                'value' => 0,
            ];
        }

        return $points;
    }

    /**
     * @return array<string, array{
     *     bucket_start: CarbonImmutable,
     *     peak_viewers: int,
     *     viewer_identity_additions: int,
     *     playlist_requests: int,
     *     segment_requests: int
     * }>
     */
    private function hourlyMetrics(
        LiveStream $liveStream,
        CarbonImmutable $start,
        CarbonImmutable $end,
    ): array {
        $metrics = [];
        $hourlyRollups = $liveStream->viewerHourlyRollups()
            ->select([
                'bucket_start',
                'peak_viewers',
                'viewer_identity_additions',
                'playlist_requests',
                'segment_requests',
            ])
            ->where('bucket_start', '>=', $start->startOfHour())
            ->where('bucket_start', '<=', $end)
            ->orderBy('bucket_start')
            ->cursor();

        /** @var LiveStreamViewerHourlyRollup $rollup */
        foreach ($hourlyRollups as $rollup) {
            if ($rollup->bucket_start === null) {
                continue;
            }

            $bucket = $rollup->bucket_start->utc()->startOfHour();
            $metrics[$bucket->format('Y-m-d-H')] = [
                'bucket_start' => $bucket,
                'peak_viewers' => $rollup->peak_viewers,
                'viewer_identity_additions' => $rollup->viewer_identity_additions,
                'playlist_requests' => $rollup->playlist_requests,
                'segment_requests' => $rollup->segment_requests,
            ];
        }

        $minuteMetrics = [];
        $minuteRollups = $liveStream->viewerRollups()
            ->select([
                'minute',
                'current_viewers',
                'peak_viewers',
                'viewer_identity_additions',
                'playlist_requests_delta',
                'segment_requests_delta',
            ])
            ->where('minute', '>=', $start)
            ->where('minute', '<=', $end)
            ->orderBy('minute')
            ->cursor();

        /** @var LiveStreamViewerRollup $rollup */
        foreach ($minuteRollups as $rollup) {
            if ($rollup->minute === null) {
                continue;
            }

            $bucket = $rollup->minute->utc()->startOfHour();
            $key = $bucket->format('Y-m-d-H');
            $aggregate = $minuteMetrics[$key] ?? [
                'bucket_start' => $bucket,
                'peak_viewers' => 0,
                'viewer_identity_additions' => 0,
                'playlist_requests' => 0,
                'segment_requests' => 0,
            ];

            $aggregate['peak_viewers'] = max(
                $aggregate['peak_viewers'],
                $rollup->peak_viewers,
                $rollup->current_viewers,
            );
            $aggregate['viewer_identity_additions'] += $rollup->viewer_identity_additions;
            $aggregate['playlist_requests'] += $rollup->playlist_requests_delta;
            $aggregate['segment_requests'] += $rollup->segment_requests_delta;
            $minuteMetrics[$key] = $aggregate;
        }

        return [...$metrics, ...$minuteMetrics];
    }

    private function overlappingBroadcastCount(
        LiveStream $liveStream,
        CarbonImmutable $start,
        CarbonImmutable $end,
    ): int {
        return $liveStream->sessions()
            ->whereNotNull('started_at')
            ->where('started_at', '<=', $end)
            ->where(function ($query) use ($start): void {
                $query->whereNull('ended_at')
                    ->orWhere('ended_at', '>=', $start);
            })
            ->count();
    }

    private function bucketKey(LiveStreamViewerPeriod $period, CarbonImmutable $timestamp): string
    {
        return match ($period) {
            LiveStreamViewerPeriod::Day => $timestamp->format('Y-m-d-H'),
            LiveStreamViewerPeriod::Month => $timestamp->format('Y-m-d'),
            LiveStreamViewerPeriod::Year => $timestamp->format('Y-m'),
        };
    }
}
