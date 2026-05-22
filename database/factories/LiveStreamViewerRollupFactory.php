<?php

namespace Database\Factories;

use App\Models\LiveStream;
use App\Models\LiveStreamSession;
use App\Models\LiveStreamViewerRollup;
use App\Models\Organization;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<LiveStreamViewerRollup>
 */
class LiveStreamViewerRollupFactory extends Factory
{
    /**
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'organization_id' => Organization::factory(),
            'live_stream_id' => LiveStream::factory(),
            'live_stream_session_id' => LiveStreamSession::factory(),
            'minute' => now()->startOfMinute(),
            'current_viewers' => fake()->numberBetween(0, 50),
            'unique_viewers_seen' => fake()->numberBetween(0, 50),
            'playlist_requests' => fake()->numberBetween(0, 500),
            'segment_requests' => fake()->numberBetween(0, 500),
        ];
    }
}
