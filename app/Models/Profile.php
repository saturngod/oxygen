<?php

namespace App\Models;

use Database\Factories\ProfileFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

#[Fillable(['organization_id', 'name', 'qualities', 'is_default', 'generate_thumbnail', 'video_segment_duration_seconds', 'live_segment_duration_seconds'])]
class Profile extends Model
{
    /** @use HasFactory<ProfileFactory> */
    use HasFactory, HasUuids;

    /**
     * @var array<string, mixed>
     */
    protected $attributes = [
        'generate_thumbnail' => false,
        'video_segment_duration_seconds' => 6,
        'live_segment_duration_seconds' => 2,
    ];

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'qualities' => 'array',
            'is_default' => 'boolean',
            'generate_thumbnail' => 'boolean',
            'video_segment_duration_seconds' => 'integer',
            'live_segment_duration_seconds' => 'integer',
        ];
    }

    public function organization(): BelongsTo
    {
        return $this->belongsTo(Organization::class);
    }
}
