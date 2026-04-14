<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('profiles', function (Blueprint $table) {
            $table->uuid('id')->primary();
            $table->foreignUuid('organization_id')->constrained()->cascadeOnDelete();
            $table->string('name');
            $table->json('qualities');
            $table->timestamps();

            $table->index(['organization_id', 'name']);
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('profiles');
    }
};
