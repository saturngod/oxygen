<?php

namespace App\Services;

use App\Models\LiveStreamViewerHourlyRollup;
use App\Models\LiveStreamViewerRollup;
use Carbon\CarbonImmutable;
use Illuminate\Support\Facades\Date;
use Illuminate\Support\Str;

class LiveStreamViewerHourlyCompactor
{
    public function compact(CarbonImmutable $start, CarbonImmutable $end): int
    {
        $start = $start->utc()->startOfHour();
        $end = $end->utc()->startOfHour();

        if ($start->greaterThanOrEqualTo($end)) {
            return 0;
        }

        $currentBucket = null;
        $aggregates = [];
        $compacted = 0;

        $rollups = LiveStreamViewerRollup::query()
            ->select([
                'organization_id',
                'live_stream_id',
                'minute',
                'current_viewers',
                'peak_viewers',
                'viewer_identity_additions',
                'playlist_requests_delta',
                'segment_requests_delta',
                'sample_count',
            ])
            ->where('minute', '>=', $start)
            ->where('minute', '<', $end)
            ->orderBy('minute')
            ->orderBy('live_stream_id')
            ->cursor();

        /** @var LiveStreamViewerRollup $rollup */
        foreach ($rollups as $rollup) {
            if ($rollup->minute === null) {
                continue;
            }

            $bucket = $rollup->minute->utc()->startOfHour();

            if ($currentBucket !== null && ! $bucket->equalTo($currentBucket)) {
                $compacted += $this->persistHour($currentBucket, $aggregates);
                $aggregates = [];
            }

            $currentBucket = $bucket;
            $streamId = $rollup->live_stream_id;
            $aggregate = $aggregates[$streamId] ?? [
                'organization_id' => $rollup->organization_id,
                'live_stream_id' => $streamId,
                'peak_viewers' => 0,
                'viewer_identity_additions' => 0,
                'playlist_requests' => 0,
                'segment_requests' => 0,
                'sample_count' => 0,
            ];

            $aggregate['peak_viewers'] = max(
                $aggregate['peak_viewers'],
                $rollup->peak_viewers,
                $rollup->current_viewers,
            );
            $aggregate['viewer_identity_additions'] += $rollup->viewer_identity_additions;
            $aggregate['playlist_requests'] += $rollup->playlist_requests_delta;
            $aggregate['segment_requests'] += $rollup->segment_requests_delta;
            $aggregate['sample_count'] += max(1, $rollup->sample_count);
            $aggregates[$streamId] = $aggregate;
        }

        if ($currentBucket !== null) {
            $compacted += $this->persistHour($currentBucket, $aggregates);
        }

        return $compacted;
    }

    /**
     * @param  array<string, array{
     *     organization_id: string,
     *     live_stream_id: string,
     *     peak_viewers: int,
     *     viewer_identity_additions: int,
     *     playlist_requests: int,
     *     segment_requests: int,
     *     sample_count: int
     * }>  $aggregates
     */
    private function persistHour(CarbonImmutable $bucket, array $aggregates): int
    {
        if ($aggregates === []) {
            return 0;
        }

        $now = Date::now();
        $rows = [];

        foreach ($aggregates as $aggregate) {
            $rows[] = [
                'id' => (string) Str::uuid(),
                ...$aggregate,
                'bucket_start' => $bucket,
                'created_at' => $now,
                'updated_at' => $now,
            ];
        }

        LiveStreamViewerHourlyRollup::query()->upsert(
            $rows,
            ['live_stream_id', 'bucket_start'],
            [
                'organization_id',
                'peak_viewers',
                'viewer_identity_additions',
                'playlist_requests',
                'segment_requests',
                'sample_count',
                'updated_at',
            ],
        );

        return count($rows);
    }
}
