<?php

namespace App\Services;

use App\Models\LiveStream;
use Illuminate\Support\Facades\Http;

class LiveStreamControlClient
{
    public function restart(LiveStream $liveStream): bool
    {
        $baseUrl = config('services.live.control_url');

        if (! is_string($baseUrl) || $baseUrl === '') {
            return false;
        }

        $request = Http::timeout(5);
        $token = config('services.live.control_token');

        if (is_string($token) && $token !== '') {
            $request = $request->withToken($token);
        }

        return $request
            ->post(rtrim($baseUrl, '/').'/streams/'.$liveStream->public_id.'/restart')
            ->successful();
    }
}
