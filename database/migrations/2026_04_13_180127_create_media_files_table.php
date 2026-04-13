<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('media_files', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('organization_id')->constrained()->cascadeOnDelete();
            $table->foreignUuid('folder_id')->nullable()->constrained()->nullOnDelete();
            $table->string('title');
            $table->string('file_name')->nullable();
            $table->string('file_path')->nullable();
            $table->string('source_url')->nullable();
            $table->string('streaming_url')->nullable();
            $table->unsignedBigInteger('size')->default(0);
            $table->string('status')->default('uploaded');
            $table->json('tags')->nullable();
            $table->timestamps();

            $table->index(['organization_id', 'folder_id']);
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('media_files');
    }
};
