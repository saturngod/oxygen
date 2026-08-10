<?php

namespace App\Services;

use App\Enums\LiveStreamViewerPeriod;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerRollup;
use Carbon\CarbonImmutable;
use Illuminate\Support\Collection;
use Illuminate\Support\Facades\Date;

class LiveStreamViewerAnalytics
{
    /**
     * @return array{
     *     range_label: string,
     *     range_start: string,
     *     range_end: string,
     *     granularity: string,
     *     source_note: string,
     *     points: list<array{timestamp: string, value: int}>,
     *     summary: array{peak_viewers: int, broadcasts: int, viewer_visits: int, playback_requests: int}
     * }
     */
    public function build(LiveStream $liveStream, LiveStreamViewerPeriod $period): array
    {
        $end = Date::now()->toImmutable()->utc();
        $start = $this->rangeStart($period, $end);
        $points = $this->emptyPoints($period, $start);
        $sessions = $this->sessions($liveStream, $start, $end);

        if ($period === LiveStreamViewerPeriod::Year) {
            $this->applySessionPeaks($points, $sessions);
        } else {
            $this->applyRollupPeaks($points, $liveStream, $period, $start, $end);
        }

        return [
            'range_label' => $period->label(),
            'range_start' => $start->toIso8601String(),
            'range_end' => $end->toIso8601String(),
            'granularity' => match ($period) {
                LiveStreamViewerPeriod::Day => 'hour',
                LiveStreamViewerPeriod::Month => 'day',
                LiveStreamViewerPeriod::Year => 'month',
            },
            'source_note' => $period === LiveStreamViewerPeriod::Year
                ? 'Monthly points use each broadcast session peak. Session summaries remain available after minute samples expire.'
                : 'Hourly and daily points use minute-by-minute viewer samples.',
            'points' => array_values($points),
            'summary' => [
                'peak_viewers' => (int) (collect($points)->max('value') ?? 0),
                'broadcasts' => $sessions->count(),
                'viewer_visits' => (int) $sessions->sum('unique_viewers'),
                'playback_requests' => (int) $sessions->sum(
                    fn (LiveStreamSession $session): int => $session->playlist_requests + $session->segment_requests,
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
     * @return Collection<int, LiveStreamSession>
     */
    private function sessions(
        LiveStream $liveStream,
        CarbonImmutable $start,
        CarbonImmutable $end,
    ): Collection {
        return $liveStream->sessions()
            ->whereBetween('started_at', [$start, $end])
            ->orderBy('started_at')
            ->get([
                'id',
                'live_stream_id',
                'started_at',
                'peak_viewers',
                'unique_viewers',
                'playlist_requests',
                'segment_requests',
            ]);
    }

    /**
     * @param  array<string, array{timestamp: string, value: int}>  $points
     * @param  Collection<int, LiveStreamSession>  $sessions
     */
    private function applySessionPeaks(array &$points, Collection $sessions): void
    {
        foreach ($sessions as $session) {
            if ($session->started_at === null) {
                continue;
            }

            $key = $this->bucketKey(LiveStreamViewerPeriod::Year, $session->started_at->utc());

            if (isset($points[$key])) {
                $points[$key]['value'] = max($points[$key]['value'], $session->peak_viewers);
            }
        }
    }

    /**
     * @param  array<string, array{timestamp: string, value: int}>  $points
     */
    private function applyRollupPeaks(
        array &$points,
        LiveStream $liveStream,
        LiveStreamViewerPeriod $period,
        CarbonImmutable $start,
        CarbonImmutable $end,
    ): void {
        $rollups = $liveStream->viewerRollups()
            ->select(['minute', 'current_viewers'])
            ->whereBetween('minute', [$start, $end])
            ->orderBy('minute')
            ->cursor();

        /** @var LiveStreamViewerRollup $rollup */
        foreach ($rollups as $rollup) {
            if ($rollup->minute === null) {
                continue;
            }

            $key = $this->bucketKey($period, $rollup->minute->utc());

            if (isset($points[$key])) {
                $points[$key]['value'] = max($points[$key]['value'], $rollup->current_viewers);
            }
        }
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
