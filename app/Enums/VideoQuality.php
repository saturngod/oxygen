<?php

namespace App\Enums;

enum VideoQuality: string
{
    case Sd240p = '240p';
    case Sd360p = '360p';
    case Sd480p = '480p';
    case Hd720p = '720p';
    case Hd1080p = '1080p';
    case Uhd1440p = '1440p';
    case Uhd2160p = '2160p';

    /**
     * @return array{category: string, label: string, width: int, height: int, bitrate_kbps: int}
     */
    public function details(): array
    {
        return match ($this) {
            self::Sd240p => ['category' => 'SD', 'label' => '240p (EGA 352 × 240)', 'width' => 352, 'height' => 240, 'bitrate_kbps' => 600],
            self::Sd360p => ['category' => 'SD', 'label' => '360p (nHD 640 × 360)', 'width' => 640, 'height' => 360, 'bitrate_kbps' => 800],
            self::Sd480p => ['category' => 'SD', 'label' => '480p (ED 842 × 480)', 'width' => 842, 'height' => 480, 'bitrate_kbps' => 1_400],
            self::Hd720p => ['category' => 'HD', 'label' => '720p (HD 1280 × 720)', 'width' => 1_280, 'height' => 720, 'bitrate_kbps' => 2_800],
            self::Hd1080p => ['category' => 'HD', 'label' => '1080p (FHD 1920 × 1080)', 'width' => 1_920, 'height' => 1_080, 'bitrate_kbps' => 5_000],
            self::Uhd1440p => ['category' => '4K', 'label' => '1440p (QHD 2560 × 1440)', 'width' => 2_560, 'height' => 1_440, 'bitrate_kbps' => 8_000],
            self::Uhd2160p => ['category' => '4K', 'label' => '2160p (UHD 3840 × 2160)', 'width' => 3_840, 'height' => 2_160, 'bitrate_kbps' => 25_000],
        };
    }

    /**
     * @return list<array{value: string, category: string, label: string, width: int, height: int, bitrate_kbps: int}>
     */
    public static function catalog(): array
    {
        return array_map(
            fn (self $quality): array => ['value' => $quality->value] + $quality->details(),
            self::cases(),
        );
    }
}
