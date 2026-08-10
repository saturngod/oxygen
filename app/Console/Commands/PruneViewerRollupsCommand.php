<?php

namespace App\Console\Commands;

use App\Models\LiveStreamViewerRollup;
use App\Services\LiveStreamViewerHourlyCompactor;
use Illuminate\Console\Command;
use Illuminate\Support\Carbon;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Date;

class PruneViewerRollupsCommand extends Command
{
    protected $signature = 'rollups:prune {--days=}';

    protected $description = 'Compact and delete per-minute viewer rollups older than the retention window';

    public function handle(LiveStreamViewerHourlyCompactor $compactor): int
    {
        $days = (int) ($this->option('days') ?? config('services.live.viewer_rollup_retention_days', 30));

        if ($days < 1) {
            $this->error('The --days option must be a positive integer.');

            return self::FAILURE;
        }

        $cutoff = Date::now()->toImmutable()->utc()->subDays($days)->startOfHour();

        $this->info("Pruning viewer rollups older than [{$cutoff->toDateTimeString()} UTC] ({$days} days).");

        $lock = Cache::lock('live-stream-viewer-rollup-maintenance', 3600);

        if (! $lock->get()) {
            $this->warn('Viewer rollup maintenance is already running.');

            return self::FAILURE;
        }

        try {
            $oldestMinute = LiveStreamViewerRollup::query()
                ->where('minute', '<', $cutoff)
                ->min('minute');

            if ($oldestMinute !== null) {
                $oldestHour = Carbon::parse($oldestMinute)->toImmutable()->utc()->startOfHour();
                $compactor->compact($oldestHour, $cutoff);
            }

            $deleted = 0;

            do {
                $ids = LiveStreamViewerRollup::query()
                    ->where('minute', '<', $cutoff)
                    ->limit(1000)
                    ->pluck('id');

                if ($ids->isNotEmpty()) {
                    $deleted += LiveStreamViewerRollup::query()->whereIn('id', $ids)->delete();
                }
            } while ($ids->isNotEmpty());

            $this->info("Deleted {$deleted} viewer rollup row(s) after hourly compaction.");
        } finally {
            $lock->release();
        }

        return self::SUCCESS;
    }
}
