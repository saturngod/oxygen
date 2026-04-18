<?php

namespace App\Models;

use App\Enums\MediaFileStatus;
use Database\Factories\MediaFileFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;
use Illuminate\Support\Facades\Storage;

#[Fillable(['organization_id', 'folder_id', 'title', 'file_name', 'file_path', 'source_url', 'streaming_url', 'size', 'status', 'progress', 'tags'])]
class MediaFile extends Model
{
    /** @use HasFactory<MediaFileFactory> */
    use HasFactory, HasUuids;

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'status' => MediaFileStatus::class,
            'progress' => 'integer',
            'tags' => 'array',
        ];
    }

    public function organization(): BelongsTo
    {
        return $this->belongsTo(Organization::class);
    }

    public function folder(): BelongsTo
    {
        return $this->belongsTo(Folder::class);
    }

    public function profiles(): HasMany
    {
        return $this->hasMany(MediaFileProfile::class);
    }

    public function fileUrl(): string
    {
        if ($this->file_path === null) {
            return '';
        }

        return Storage::disk('s3')->url($this->file_path);
    }
}
