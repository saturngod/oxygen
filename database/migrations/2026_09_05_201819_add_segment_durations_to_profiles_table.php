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
        Schema::table('profiles', function (Blueprint $table) {
            $table->unsignedTinyInteger('video_segment_duration_seconds')->default(6)->after('generate_thumbnail');
            $table->unsignedTinyInteger('live_segment_duration_seconds')->default(2)->after('video_segment_duration_seconds');
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('profiles', function (Blueprint $table) {
            $table->dropColumn(['video_segment_duration_seconds', 'live_segment_duration_seconds']);
        });
    }
};
