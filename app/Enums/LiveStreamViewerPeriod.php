<?php

namespace App\Enums;

enum LiveStreamViewerPeriod: string
{
    case Day = 'day';
    case Month = 'month';
    case Year = 'year';

    public function label(): string
    {
        return match ($this) {
            self::Day => 'Last 24 hours',
            self::Month => 'Last 30 days',
            self::Year => 'Last 12 months',
        };
    }
}
