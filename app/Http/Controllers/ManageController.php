<?php

namespace App\Http\Controllers;

use App\Enums\MediaFileStatus;
use App\Http\Requests\Manage\StoreFolderRequest;
use App\Http\Requests\Manage\StoreMediaFileRequest;
use App\Http\Requests\Manage\StoreMediaUrlRequest;
use App\Models\Folder;
use App\Models\MediaFile;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Inertia\Inertia;
use Inertia\Response;

class ManageController extends Controller
{
    public function index(Request $request): Response
    {
        $organizationId = $this->currentOrganizationId($request);

        $folderId = $request->query('folder');
        $currentFolder = null;

        if ($folderId !== null) {
            $currentFolder = Folder::query()
                ->where('organization_id', $organizationId)
                ->findOrFail($folderId);
        }

        $folders = Folder::query()
            ->where('organization_id', $organizationId)
            ->orderBy('name')
            ->get()
            ->map(fn (Folder $folder) => [
                'id' => $folder->id,
                'name' => $folder->name,
            ]);

        $files = MediaFile::query()
            ->where('organization_id', $organizationId)
            ->where('folder_id', $currentFolder?->id)
            ->orderByDesc('created_at')
            ->get()
            ->map(fn (MediaFile $file) => [
                'id' => $file->id,
                'title' => $file->title,
                'file_name' => $file->file_name,
                'source_url' => $file->source_url,
                'streaming_url' => $file->streaming_url,
                'status' => $file->status->value,
                'tags' => $file->tags ?? [],
                'size' => $file->size,
                'created_at' => $file->created_at?->toIso8601String(),
            ]);

        return Inertia::render('manage', [
            'currentFolder' => $currentFolder === null ? null : [
                'id' => $currentFolder->id,
                'name' => $currentFolder->name,
            ],
            'folders' => $folders,
            'files' => $files,
        ]);
    }

    public function storeFolder(StoreFolderRequest $request): RedirectResponse
    {
        Folder::create([
            'organization_id' => $this->currentOrganizationId($request),
            'name' => $request->string('name'),
        ]);

        return back()->with('toast', ['type' => 'success', 'message' => __('Folder created.')]);
    }

    public function storeFile(StoreMediaFileRequest $request): RedirectResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        $folderId = $request->input('folder_id');
        if ($folderId !== null) {
            Folder::query()
                ->where('organization_id', $organizationId)
                ->findOrFail($folderId);
        }

        $uploaded = $request->file('file');
        $path = $uploaded->store('media/'.$organizationId, 'public');

        MediaFile::create([
            'organization_id' => $organizationId,
            'folder_id' => $folderId,
            'title' => $request->string('title'),
            'file_name' => $uploaded->getClientOriginalName(),
            'file_path' => $path,
            'size' => $uploaded->getSize(),
            'status' => MediaFileStatus::Uploaded,
            'tags' => $request->input('tags', []),
        ]);

        return back()->with('toast', ['type' => 'success', 'message' => __('File uploaded.')]);
    }

    public function storeFromUrl(StoreMediaUrlRequest $request): RedirectResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        $folderId = $request->input('folder_id');
        if ($folderId !== null) {
            Folder::query()
                ->where('organization_id', $organizationId)
                ->findOrFail($folderId);
        }

        $sourceUrl = (string) $request->string('source_url');

        MediaFile::create([
            'organization_id' => $organizationId,
            'folder_id' => $folderId,
            'title' => $request->string('title'),
            'file_name' => basename(parse_url($sourceUrl, PHP_URL_PATH) ?: ''),
            'source_url' => $sourceUrl,
            'status' => MediaFileStatus::Uploaded,
            'tags' => $request->input('tags', []),
        ]);

        return back()->with('toast', ['type' => 'success', 'message' => __('Video queued from URL.')]);
    }

    private function currentOrganizationId(Request $request): string
    {
        $id = $request->session()->get('current_organization_id');

        abort_if($id === null, 403, 'No active organization.');

        return (string) $id;
    }
}
