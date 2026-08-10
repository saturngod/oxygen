<?php

namespace App\Console\Commands;

use App\Services\LiveStreamViewerHourlyCompactor;
use Illuminate\Console\Attributes\Description;
use Illuminate\Console\Attributes\Signature;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Date;

#[Signature('rollups:compact-hourly {--hours=}')]
#[Description('Compact recent per-minute viewer rollups into durable UTC hourly buckets')]
class CompactViewerRollupsCommand extends Command
{
    public function handle(LiveStreamViewerHourlyCompactor $compactor): int
    {
        $hours = (int) ($this->option('hours') ?? config('services.live.viewer_compaction_lookback_hours', 48));

        if ($hours < 1) {
            $this->error('The --hours option must be a positive integer.');

            return self::FAILURE;
        }

        $lock = Cache::lock('live-stream-viewer-rollup-maintenance', 3600);

        if (! $lock->get()) {
            $this->warn('Viewer rollup maintenance is already running.');

            return self::FAILURE;
        }

        try {
            $end = Date::now()->toImmutable()->utc()->startOfHour();
            $start = $end->subHours($hours);
            $compacted = $compactor->compact($start, $end);

            $this->info("Compacted {$compacted} stream-hour row(s) from {$start->toDateTimeString()} UTC to {$end->toDateTimeString()} UTC.");
        } finally {
            $lock->release();
        }

        return self::SUCCESS;
    }
}
