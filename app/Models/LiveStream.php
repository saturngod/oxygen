<?php

namespace App\Models;

use App\Enums\LiveStreamStatus;
use Database\Factories\LiveStreamFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;

#[Fillable([
    'organization_id',
    'created_by_id',
    'title',
    'public_id',
    'stream_key',
    'status',
    'recording_enabled',
    'restart_required',
    'settings_version',
    'active_session_id',
    'rtmp_url',
    'hls_url',
    'last_started_at',
    'last_ended_at',
])]
class LiveStream extends Model
{
    /** @use HasFactory<LiveStreamFactory> */
    use HasFactory, HasUuids;

    /**
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'stream_key' => 'encrypted',
            'status' => LiveStreamStatus::class,
            'recording_enabled' => 'boolean',
            'restart_required' => 'boolean',
            'settings_version' => 'integer',
            'last_started_at' => 'immutable_datetime',
            'last_ended_at' => 'immutable_datetime',
        ];
    }

    public function organization(): BelongsTo
    {
        return $this->belongsTo(Organization::class);
    }

    public function creator(): BelongsTo
    {
        return $this->belongsTo(User::class, 'created_by_id');
    }

    public function sessions(): HasMany
    {
        return $this->hasMany(LiveStreamSession::class);
    }

    public function viewerRollups(): HasMany
    {
        return $this->hasMany(LiveStreamViewerRollup::class);
    }

    public function isLive(): bool
    {
        return $this->status === LiveStreamStatus::Live;
    }
}
