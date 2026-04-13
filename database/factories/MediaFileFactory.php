<?php

namespace Database\Factories;

use App\Enums\MediaFileStatus;
use App\Models\MediaFile;
use App\Models\Organization;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<MediaFile>
 */
class MediaFileFactory extends Factory
{
    /**
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'organization_id' => Organization::factory(),
            'folder_id' => null,
            'title' => fake()->sentence(3),
            'file_name' => fake()->slug().'.mp4',
            'file_path' => 'media/example.mp4',
            'streaming_url' => null,
            'size' => fake()->numberBetween(1024, 1024 * 1024),
            'status' => MediaFileStatus::Uploaded,
            'tags' => [],
        ];
    }
}
