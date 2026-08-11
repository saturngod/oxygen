<?php

namespace App\Contracts;

use App\Enums\LiveStreamViewerPeriod;
use App\Models\LiveStream;

interface LiveStreamAnalyticsReader
{
    /**
     * @return array<string, mixed>
     */
    public function build(LiveStream $liveStream, LiveStreamViewerPeriod $period): array;

    /**
     * @return array<string, mixed>|null
     */
    public function current(LiveStream $liveStream): ?array;
}
