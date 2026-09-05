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
        Schema::table('media_file_profiles', function (Blueprint $table) {
            $table->unsignedTinyInteger('video_segment_duration_seconds')->default(6)->after('qualities');
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('media_file_profiles', function (Blueprint $table) {
            $table->dropColumn('video_segment_duration_seconds');
        });
    }
};
