<?php

namespace App\Enums;

enum LiveStreamSessionStatus: string
{
    case Starting = 'starting';
    case Live = 'live';
    case Ended = 'ended';
    case Failed = 'failed';
}
