<?php

namespace App\Services;

use App\Models\LiveStream;
use Illuminate\Http\Client\ConnectionException;
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

        try {
            return $request
                ->post(rtrim($baseUrl, '/').'/streams/'.$liveStream->public_id.'/restart')
                ->successful();
        } catch (ConnectionException) {
            // Live service unreachable/timed out: report failure to the caller
            // instead of bubbling a 500 up to the admin.
            return false;
        }
    }
}
