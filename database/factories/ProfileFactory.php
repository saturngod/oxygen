<?php

namespace Database\Factories;

use App\Enums\VideoQuality;
use App\Models\Organization;
use App\Models\Profile;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<Profile>
 */
class ProfileFactory extends Factory
{
    /**
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'organization_id' => Organization::factory(),
            'name' => fake()->words(2, true),
            'qualities' => [VideoQuality::Hd720p->value, VideoQuality::Hd1080p->value],
            'is_default' => false,
        ];
    }
}
