<?php

namespace Database\Factories;

use App\Enums\LiveStreamSessionStatus;
use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<LiveStreamSession>
 */
class LiveStreamSessionFactory extends Factory
{
    /**
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'live_stream_id' => LiveStream::factory(),
            'external_id' => fake()->uuid(),
            'status' => LiveStreamSessionStatus::Live,
            'settings_version' => 1,
            'recording_enabled' => false,
            'hls_url' => 'https://live.test/live/example/index.m3u8',
            'hls_prefix' => 'live/example',
            'recording_path' => null,
            'current_viewers' => 0,
            'peak_viewers' => 0,
            'unique_viewers' => 0,
            'playlist_requests' => 0,
            'segment_requests' => 0,
            'started_at' => now(),
            'ended_at' => null,
            'error_message' => null,
        ];
    }
}
