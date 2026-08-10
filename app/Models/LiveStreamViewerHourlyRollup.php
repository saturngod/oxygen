<?php

namespace App\Models;

use Database\Factories\LiveStreamViewerHourlyRollupFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

#[Fillable([
    'organization_id',
    'live_stream_id',
    'bucket_start',
    'peak_viewers',
    'viewer_identity_additions',
    'playlist_requests',
    'segment_requests',
    'sample_count',
])]
class LiveStreamViewerHourlyRollup extends Model
{
    /** @use HasFactory<LiveStreamViewerHourlyRollupFactory> */
    use HasFactory, HasUuids;

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'bucket_start' => 'immutable_datetime',
            'peak_viewers' => 'integer',
            'viewer_identity_additions' => 'integer',
            'playlist_requests' => 'integer',
            'segment_requests' => 'integer',
            'sample_count' => 'integer',
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
}
