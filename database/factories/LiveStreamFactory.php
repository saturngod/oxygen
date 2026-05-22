<?php

namespace Database\Factories;

use App\Enums\LiveStreamStatus;
use App\Models\LiveStream;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Database\Eloquent\Factories\Factory;
use Illuminate\Support\Str;

/**
 * @extends Factory<LiveStream>
 */
class LiveStreamFactory extends Factory
{
    /**
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        $publicId = Str::lower((string) Str::ulid());

        return [
            'organization_id' => Organization::factory(),
            'created_by_id' => User::factory(),
            'title' => fake()->sentence(3),
            'public_id' => $publicId,
            'stream_key' => Str::random(48),
            'status' => LiveStreamStatus::Idle,
            'recording_enabled' => false,
            'restart_required' => false,
            'settings_version' => 1,
            'active_session_id' => null,
            'rtmp_url' => 'rtmp://live.test/live',
            'hls_url' => "https://live.test/live/{$publicId}/index.m3u8",
            'last_started_at' => null,
            'last_ended_at' => null,
        ];
    }

    public function live(): static
    {
        return $this->state(fn (array $attributes) => [
            'status' => LiveStreamStatus::Live,
            'last_started_at' => now(),
        ]);
    }
}
