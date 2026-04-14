<?php

namespace Database\Factories;

use App\Enums\VideoQuality;
use App\Models\MediaFile;
use App\Models\MediaFileProfile;
use App\Models\Profile;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<MediaFileProfile>
 */
class MediaFileProfileFactory extends Factory
{
    /**
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'media_file_id' => MediaFile::factory(),
            'profile_id' => Profile::factory(),
            'name' => fake()->words(2, true),
            'qualities' => [VideoQuality::Hd720p->value, VideoQuality::Hd1080p->value],
        ];
    }
}
