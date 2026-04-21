<?php

namespace App\Http\Controllers;

use App\Models\MediaFile;
use Illuminate\Http\Request;
use Inertia\Inertia;
use Inertia\Response;

class StatusController extends Controller
{
    public function __invoke(Request $request): Response
    {
        $organizationId = $request->session()->get('current_organization_id');
        abort_if($organizationId === null, 403, 'No active organization.');

        $files = MediaFile::query()
            ->where('organization_id', $organizationId)
            ->with('profiles:id,media_file_id,name,qualities')
            ->orderByDesc('created_at')
            ->limit(20)
            ->get()
            ->map(fn (MediaFile $file) => [
                'id' => $file->id,
                'title' => $file->title,
                'file_name' => $file->file_name,
                'source_url' => $file->source_url,
                'streaming_url' => $file->streaming_url,
                'status' => $file->status->value,
                'progress' => $file->progress,
                'size' => $file->size,
                'tags' => $file->tags ?? [],
                'created_at' => $file->created_at?->toIso8601String(),
                'profiles' => $file->profiles->map(fn ($profile) => [
                    'id' => $profile->id,
                    'name' => $profile->name,
                    'qualities' => $profile->qualities,
                ])->all(),
            ]);

        return Inertia::render('status', [
            'files' => $files,
        ]);
    }
}
