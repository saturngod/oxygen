<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::create('live_stream_viewer_hourly_rollups', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('organization_id')->constrained()->cascadeOnDelete();
            $table->foreignUuid('live_stream_id')->constrained()->cascadeOnDelete();
            $table->timestamp('bucket_start');
            $table->unsignedInteger('peak_viewers')->default(0);
            $table->unsignedBigInteger('viewer_identity_additions')->default(0);
            $table->unsignedBigInteger('playlist_requests')->default(0);
            $table->unsignedBigInteger('segment_requests')->default(0);
            $table->unsignedInteger('sample_count')->default(0);
            $table->timestamps();

            $table->unique(['live_stream_id', 'bucket_start']);
            $table->index(['organization_id', 'bucket_start']);
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('live_stream_viewer_hourly_rollups');
    }
};
