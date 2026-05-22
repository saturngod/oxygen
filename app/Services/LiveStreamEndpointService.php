<?php

namespace App\Services;

use App\Models\LiveStream;
use Illuminate\Support\Str;

class LiveStreamEndpointService
{
    public function generatePublicId(): string
    {
        return Str::lower((string) Str::ulid());
    }

    public function generateStreamKey(): string
    {
        return Str::random(48);
    }

    public function rtmpUrl(): string
    {
        return rtrim((string) config('services.live.rtmp_url'), '/');
    }

    public function hlsUrl(string $publicId): string
    {
        return rtrim((string) config('services.live.hls_url'), '/').'/'.$publicId.'/index.m3u8';
    }

    public function streamPath(LiveStream $liveStream): string
    {
        return $liveStream->public_id;
    }
}
