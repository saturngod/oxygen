<?php

namespace App\Models;

use App\Enums\LiveStreamSessionStatus;
use Database\Factories\LiveStreamSessionFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;

#[Fillable([
    'live_stream_id',
    'external_id',
    'status',
    'settings_version',
    'recording_enabled',
    'hls_url',
    'hls_prefix',
    'recording_path',
    'current_viewers',
    'peak_viewers',
    'unique_viewers',
    'playlist_requests',
    'segment_requests',
    'started_at',
    'ended_at',
    'error_message',
])]
class LiveStreamSession extends Model
{
    /** @use HasFactory<LiveStreamSessionFactory> */
    use HasFactory, HasUuids;

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'status' => LiveStreamSessionStatus::class,
            'settings_version' => 'integer',
            'recording_enabled' => 'boolean',
            'current_viewers' => 'integer',
            'peak_viewers' => 'integer',
            'unique_viewers' => 'integer',
            'playlist_requests' => 'integer',
            'segment_requests' => 'integer',
            'started_at' => 'immutable_datetime',
            'ended_at' => 'immutable_datetime',
        ];
    }

    public function liveStream(): BelongsTo
    {
        return $this->belongsTo(LiveStream::class);
    }

    public function viewerRollups(): HasMany
    {
        return $this->hasMany(LiveStreamViewerRollup::class);
    }
}
