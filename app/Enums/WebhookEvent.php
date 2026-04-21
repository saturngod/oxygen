<?php

namespace App\Enums;

enum WebhookEvent: string
{
    case FileUploaded = 'file_uploaded';
    case FileStatusChanged = 'file_status_changed';

    public function label(): string
    {
        return match ($this) {
            self::FileUploaded => 'New File Upload',
            self::FileStatusChanged => 'File Status Change',
        };
    }
}
