<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('media_file_profiles', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('media_file_id')->constrained()->cascadeOnDelete();
            $table->foreignUuid('profile_id')->nullable()->constrained('profiles')->nullOnDelete();
            $table->string('name');
            $table->json('qualities');
            $table->timestamps();

            $table->index('media_file_id');
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('media_file_profiles');
    }
};
