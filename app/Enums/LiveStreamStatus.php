<?php

namespace App\Enums;

enum LiveStreamStatus: string
{
    case Idle = 'idle';
    case Live = 'live';
    case Offline = 'offline';
    case Restarting = 'restarting';
    case Failed = 'failed';
    case Disabled = 'disabled';

    public function label(): string
    {
        return match ($this) {
            self::Idle => 'Idle',
            self::Live => 'Live',
            self::Offline => 'Offline',
            self::Restarting => 'Restarting',
            self::Failed => 'Failed',
            self::Disabled => 'Disabled',
        };
    }
}
