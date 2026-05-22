<?php

namespace App\Models;

use Database\Factories\LiveStreamViewerRollupFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

#[Fillable([
    'organization_id',
    'live_stream_id',
    'live_stream_session_id',
    'minute',
    'current_viewers',
    'unique_viewers_seen',
    'playlist_requests',
    'segment_requests',
])]
class LiveStreamViewerRollup extends Model
{
    /** @use HasFactory<LiveStreamViewerRollupFactory> */
    use HasFactory, HasUuids;

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'minute' => 'immutable_datetime',
            'current_viewers' => 'integer',
            'unique_viewers_seen' => 'integer',
            'playlist_requests' => 'integer',
            'segment_requests' => 'integer',
        ];
    }

    public function organization(): BelongsTo
    {
        return $this->belongsTo(Organization::class);
    }

    public function liveStream(): BelongsTo
    {
        return $this->belongsTo(LiveStream::class);
    }

    public function liveStreamSession(): BelongsTo
    {
        return $this->belongsTo(LiveStreamSession::class);
    }
}
