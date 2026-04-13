<?php

namespace App\Http\Controllers;

use App\Enums\MediaFileStatus;
use App\Http\Requests\Manage\FinalizeChunkUploadRequest;
use App\Http\Requests\Manage\StoreFolderRequest;
use App\Http\Requests\Manage\StoreMediaFileRequest;
use App\Http\Requests\Manage\StoreMediaUrlRequest;
use App\Http\Requests\Manage\UploadChunkRequest;
use App\Models\Folder;
use App\Models\MediaFile;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Storage;
use Illuminate\Support\Str;
use Inertia\Inertia;
use Inertia\Response;

class ManageController extends Controller
{
    private const MEDIA_DISK = 's3';

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
        $path = $uploaded->store('media/'.$organizationId, self::MEDIA_DISK);

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

    public function uploadChunk(UploadChunkRequest $request): JsonResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        $uploadId = (string) $request->string('upload_id');
        $index = (int) $request->integer('chunk_index');
        $total = (int) $request->integer('total_chunks');

        $disk = Storage::disk('local');
        $dir = "chunks/{$organizationId}/{$uploadId}";
        $disk->makeDirectory($dir);

        $request->file('chunk')->storeAs($dir, (string) $index, 'local');

        $received = collect($disk->files($dir))
            ->map(fn (string $path) => (int) basename($path))
            ->sort()
            ->values()
            ->all();

        return response()->json([
            'upload_id' => $uploadId,
            'received' => $received,
            'total' => $total,
        ]);
    }

    public function finalizeChunkUpload(FinalizeChunkUploadRequest $request): RedirectResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        $folderId = $request->input('folder_id');
        if ($folderId !== null) {
            Folder::query()
                ->where('organization_id', $organizationId)
                ->findOrFail($folderId);
        }

        $uploadId = (string) $request->string('upload_id');
        $total = (int) $request->integer('total_chunks');
        $originalName = (string) $request->string('file_name');

        $extension = strtolower(pathinfo($originalName, PATHINFO_EXTENSION));
        abort_unless(in_array($extension, ['mp4', 'mov'], true), 422, 'Unsupported file extension.');

        $local = Storage::disk('local');
        $dir = "chunks/{$organizationId}/{$uploadId}";

        abort_unless($local->exists($dir), 404, 'Upload session not found.');

        for ($i = 0; $i < $total; $i++) {
            abort_unless($local->exists("{$dir}/{$i}"), 422, "Missing chunk {$i}.");
        }

        $targetRelative = 'media/'.$organizationId.'/'.Str::uuid()->toString().'.'.$extension;

        $tempPath = tempnam(sys_get_temp_dir(), 'upload-');
        abort_if($tempPath === false, 500, 'Could not allocate temp file.');

        $tempHandle = fopen($tempPath, 'wb');
        abort_if($tempHandle === false, 500, 'Could not open temp file.');

        try {
            for ($i = 0; $i < $total; $i++) {
                $chunkStream = $local->readStream("{$dir}/{$i}");
                if ($chunkStream === null) {
                    throw new \RuntimeException("Unable to read chunk {$i}.");
                }
                stream_copy_to_stream($chunkStream, $tempHandle);
                fclose($chunkStream);
            }
        } finally {
            fclose($tempHandle);
        }

        $media = Storage::disk(self::MEDIA_DISK);
        $uploadStream = fopen($tempPath, 'rb');
        abort_if($uploadStream === false, 500, 'Could not re-open temp file.');

        try {
            $media->writeStream($targetRelative, $uploadStream);
        } finally {
            if (is_resource($uploadStream)) {
                fclose($uploadStream);
            }
        }

        $size = filesize($tempPath) ?: 0;

        @unlink($tempPath);
        $local->deleteDirectory($dir);

        MediaFile::create([
            'organization_id' => $organizationId,
            'folder_id' => $folderId,
            'title' => $request->string('title'),
            'file_name' => $originalName,
            'file_path' => $targetRelative,
            'size' => $size,
            'status' => MediaFileStatus::Uploaded,
            'tags' => $request->input('tags', []),
        ]);

        return back()->with('toast', ['type' => 'success', 'message' => __('File uploaded.')]);
    }

    public function cancelChunkUpload(Request $request, string $uploadId): JsonResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        abort_unless(preg_match('/^[a-zA-Z0-9\-]{8,64}$/', $uploadId) === 1, 422);

        Storage::disk('local')->deleteDirectory("chunks/{$organizationId}/{$uploadId}");

        return response()->json(['ok' => true]);
    }

    private function currentOrganizationId(Request $request): string
    {
        $id = $request->session()->get('current_organization_id');

        abort_if($id === null, 403, 'No active organization.');

        return (string) $id;
    }
}
