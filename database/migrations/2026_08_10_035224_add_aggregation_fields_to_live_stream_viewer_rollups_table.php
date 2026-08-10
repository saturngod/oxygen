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
        Schema::table('live_stream_viewer_rollups', function (Blueprint $table) {
            $table->unsignedInteger('peak_viewers')->default(0);
            $table->unsignedBigInteger('viewer_identity_additions')->default(0);
            $table->unsignedBigInteger('playlist_requests_delta')->default(0);
            $table->unsignedBigInteger('segment_requests_delta')->default(0);
            $table->unsignedInteger('sample_count')->default(0);

            $table->index('minute');
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('live_stream_viewer_rollups', function (Blueprint $table) {
            $table->dropIndex(['minute']);
            $table->dropColumn([
                'peak_viewers',
                'viewer_identity_additions',
                'playlist_requests_delta',
                'segment_requests_delta',
                'sample_count',
            ]);
        });
    }
};
