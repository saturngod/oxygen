<?php

namespace App\Enums;

enum MediaFileStatus: string
{
    case Uploaded = 'uploaded';
    case Progress = 'progress';
    case Success = 'success';
    case Failed = 'failed';
}
