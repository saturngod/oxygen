<?php

namespace Database\Factories;

use App\Models\LiveStream;
use App\Models\LiveStreamViewerHourlyRollup;
use App\Models\Organization;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<LiveStreamViewerHourlyRollup>
 */
class LiveStreamViewerHourlyRollupFactory extends Factory
{
    /**
     * Define the model's default state.
     *
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'organization_id' => Organization::factory(),
            'live_stream_id' => LiveStream::factory(),
            'bucket_start' => now()->startOfHour(),
            'peak_viewers' => fake()->numberBetween(0, 50),
            'viewer_identity_additions' => fake()->numberBetween(0, 100),
            'playlist_requests' => fake()->numberBetween(0, 500),
            'segment_requests' => fake()->numberBetween(0, 500),
            'sample_count' => fake()->numberBetween(1, 240),
        ];
    }
}
