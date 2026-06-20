<?php

namespace App\Console\Commands;

use App\Models\LiveStreamViewerRollup;
use Illuminate\Console\Command;

class PruneViewerRollupsCommand extends Command
{
    protected $signature = 'rollups:prune {--days=30}';

    protected $description = 'Delete per-minute live stream viewer rollups older than the retention window (session summaries are kept)';

    public function handle(): int
    {
        $days = (int) $this->option('days');

        if ($days < 1) {
            $this->error('The --days option must be a positive integer.');

            return self::FAILURE;
        }

        $cutoff = now()->subDays($days)->startOfMinute();

        $this->info("Pruning viewer rollups older than [{$cutoff->toDateTimeString()}] ({$days} days).");

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

        $this->info("Deleted {$deleted} viewer rollup row(s).");

        return self::SUCCESS;
    }
}
