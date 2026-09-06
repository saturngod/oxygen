<?php

namespace App\Contracts;

use App\Models\LiveStream;

interface LiveStreamAnalyticsPurger
{
    public function purge(LiveStream $liveStream): bool;
}
