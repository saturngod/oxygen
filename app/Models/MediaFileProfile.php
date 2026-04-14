<?php

namespace App\Models;

use Database\Factories\MediaFileProfileFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

#[Fillable(['media_file_id', 'profile_id', 'name', 'qualities'])]
class MediaFileProfile extends Model
{
    /** @use HasFactory<MediaFileProfileFactory> */
    use HasFactory, HasUuids;

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'qualities' => 'array',
        ];
    }

    public function mediaFile(): BelongsTo
    {
        return $this->belongsTo(MediaFile::class);
    }

    public function profile(): BelongsTo
    {
        return $this->belongsTo(Profile::class);
    }
}
