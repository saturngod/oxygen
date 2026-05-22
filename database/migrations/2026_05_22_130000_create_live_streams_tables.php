<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('live_streams', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('organization_id')->constrained()->cascadeOnDelete();
            $table->foreignId('created_by_id')->nullable()->constrained('users')->nullOnDelete();
            $table->string('title');
            $table->string('public_id')->unique();
            $table->text('stream_key');
            $table->string('status')->default('idle');
            $table->boolean('recording_enabled')->default(false);
            $table->boolean('restart_required')->default(false);
            $table->unsignedInteger('settings_version')->default(1);
            $table->uuid('active_session_id')->nullable();
            $table->string('rtmp_url');
            $table->string('hls_url');
            $table->timestamp('last_started_at')->nullable();
            $table->timestamp('last_ended_at')->nullable();
            $table->timestamps();

            $table->index(['organization_id', 'status']);
            $table->index(['organization_id', 'created_at']);
        });

        Schema::create('live_stream_sessions', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('live_stream_id')->constrained()->cascadeOnDelete();
            $table->string('external_id')->nullable()->index();
            $table->string('status')->default('starting');
            $table->unsignedInteger('settings_version');
            $table->boolean('recording_enabled')->default(false);
            $table->string('hls_url')->nullable();
            $table->string('hls_prefix')->nullable();
            $table->string('recording_path')->nullable();
            $table->unsignedInteger('current_viewers')->default(0);
            $table->unsignedInteger('peak_viewers')->default(0);
            $table->unsignedInteger('unique_viewers')->default(0);
            $table->unsignedBigInteger('playlist_requests')->default(0);
            $table->unsignedBigInteger('segment_requests')->default(0);
            $table->timestamp('started_at')->nullable();
            $table->timestamp('ended_at')->nullable();
            $table->text('error_message')->nullable();
            $table->timestamps();

            $table->index(['live_stream_id', 'started_at']);
            $table->index(['status', 'started_at']);
        });

        Schema::create('live_stream_viewer_rollups', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('organization_id')->constrained()->cascadeOnDelete();
            $table->foreignUuid('live_stream_id')->constrained()->cascadeOnDelete();
            $table->foreignUuid('live_stream_session_id')->constrained()->cascadeOnDelete();
            $table->timestamp('minute');
            $table->unsignedInteger('current_viewers')->default(0);
            $table->unsignedInteger('unique_viewers_seen')->default(0);
            $table->unsignedBigInteger('playlist_requests')->default(0);
            $table->unsignedBigInteger('segment_requests')->default(0);
            $table->timestamps();

            $table->unique(['live_stream_session_id', 'minute']);
            $table->index(['organization_id', 'minute']);
            $table->index(['live_stream_id', 'minute']);
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('live_stream_viewer_rollups');
        Schema::dropIfExists('live_stream_sessions');
        Schema::dropIfExists('live_streams');
    }
};
