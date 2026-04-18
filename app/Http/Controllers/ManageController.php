<?php

namespace App\Http\Controllers;

use App\Enums\MediaFileStatus;
use App\Http\Requests\Manage\StoreFolderRequest;
use App\Http\Requests\Manage\StoreMediaUrlRequest;
use App\Models\Folder;
use App\Models\MediaFile;
use App\Models\Profile;
use App\Services\S3MultipartUploadManager;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Redis;
use Illuminate\Support\Facades\Storage;
use Illuminate\Support\Str;
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
            ->with('profiles:id,media_file_id,name,qualities')
            ->orderByDesc('created_at')
            ->get()
            ->map(fn (MediaFile $file) => [
                'id' => $file->id,
                'title' => $file->title,
                'file_name' => $file->file_name,
                'source_url' => $file->source_url,
                'streaming_url' => $file->streaming_url,
                'status' => $file->status->value,
                'progress' => $file->progress,
                'tags' => $file->tags ?? [],
                'size' => $file->size,
                'created_at' => $file->created_at?->toIso8601String(),
                'profiles' => $file->profiles->map(fn ($profile) => [
                    'id' => $profile->id,
                    'name' => $profile->name,
                    'qualities' => $profile->qualities,
                ])->all(),
            ]);

        $profiles = Profile::query()
            ->where('organization_id', $organizationId)
            ->orderByDesc('is_default')
            ->orderBy('name')
            ->get(['id', 'name', 'qualities', 'is_default'])
            ->map(fn (Profile $profile): array => [
                'id' => $profile->id,
                'name' => $profile->name,
                'qualities' => $profile->qualities,
                'is_default' => $profile->is_default,
            ])
            ->all();

        return Inertia::render('manage', [
            'currentFolder' => $currentFolder === null ? null : [
                'id' => $currentFolder->id,
                'name' => $currentFolder->name,
            ],
            'folders' => $folders,
            'files' => $files,
            'profiles' => $profiles,
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

    public function destroyFile(Request $request, MediaFile $mediaFile): RedirectResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        abort_unless($mediaFile->organization_id === $organizationId, 403);
        abort_if(
            $mediaFile->status === MediaFileStatus::Progress,
            422,
            __('Cannot delete a file while it is being processed.'),
        );

        if ($mediaFile->file_path !== null) {
            Storage::disk('s3')->delete($mediaFile->file_path);
        }

        $mediaFile->delete();

        return back()->with('toast', ['type' => 'success', 'message' => __('File deleted.')]);
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

        $profile = $this->resolveProfile($organizationId, $request->string('profile_id'));

        $sourceUrl = (string) $request->string('source_url');

        DB::transaction(function () use ($request, $organizationId, $folderId, $sourceUrl, $profile): void {
            $mediaFile = MediaFile::create([
                'organization_id' => $organizationId,
                'folder_id' => $folderId,
                'title' => $request->string('title'),
                'file_name' => basename(parse_url($sourceUrl, PHP_URL_PATH) ?: ''),
                'source_url' => $sourceUrl,
                'status' => MediaFileStatus::Uploaded,
                'tags' => $request->input('tags', []),
            ]);

            $this->attachProfileSnapshot($mediaFile, $profile);
            $this->dispatchTranscodeJob($mediaFile);
        });

        return back()->with('toast', ['type' => 'success', 'message' => __('Video queued from URL.')]);
    }

    public function initMultipartUpload(Request $request, S3MultipartUploadManager $s3): JsonResponse
    {
        $organizationId = $this->currentOrganizationId($request);

        $validated = $request->validate([
            'file_name' => ['required', 'string', 'max:255'],
            'folder_id' => ['nullable', 'uuid', 'exists:folders,id'],
            'profile_id' => ['required', 'uuid'],
        ]);

        if ($validated['folder_id'] ?? null) {
            Folder::query()
                ->where('organization_id', $organizationId)
                ->findOrFail($validated['folder_id']);
        }

        $profile = $this->resolveProfile($organizationId, $validated['profile_id']);

        $originalName = $validated['file_name'];
        $extension = strtolower(pathinfo($originalName, PATHINFO_EXTENSION));
        abort_unless(in_array($extension, ['mp4', 'mov'], true), 422, 'Unsupported file extension.');

        $contentType = $extension === 'mov' ? 'video/quicktime' : 'video/mp4';
        $key = 'media/'.$organizationId.'/'.Str::uuid()->toString().'.'.$extension;

        $uploadId = $s3->initiate($key, $contentType);

        Cache::put($this->uploadCacheKey($uploadId), [
            'organization_id' => $organizationId,
            'user_id' => $request->user()?->getKey(),
            'folder_id' => $validated['folder_id'] ?? null,
            'key' => $key,
            'file_name' => $originalName,
            'profile_id' => $profile->id,
            'profile_name' => $profile->name,
            'profile_qualities' => $profile->qualities,
        ], now()->addHours(24));

        return response()->json([
            'upload_id' => $uploadId,
            'key' => $key,
        ]);
    }

    public function signPart(Request $request, S3MultipartUploadManager $s3): JsonResponse
    {
        $validated = $request->validate([
            'upload_id' => ['required', 'string'],
            'part_number' => ['required', 'integer', 'min:1', 'max:10000'],
        ]);

        $session = $this->authorizeUploadSession($request, $validated['upload_id']);

        $url = $s3->presignPart($session['key'], $validated['upload_id'], (int) $validated['part_number']);

        return response()->json(['url' => $url]);
    }

    public function completeMultipartUpload(Request $request, S3MultipartUploadManager $s3): JsonResponse
    {
        $validated = $request->validate([
            'upload_id' => ['required', 'string'],
            'title' => ['required', 'string', 'max:255'],
            'parts' => ['required', 'array', 'min:1'],
            'parts.*.part_number' => ['required', 'integer', 'min:1', 'max:10000'],
            'parts.*.etag' => ['required', 'string'],
            'tags' => ['nullable', 'array'],
            'tags.*' => ['string', 'max:50'],
        ]);

        $session = $this->authorizeUploadSession($request, $validated['upload_id']);

        $parts = array_map(fn (array $p) => [
            'PartNumber' => (int) $p['part_number'],
            'ETag' => (string) $p['etag'],
        ], $validated['parts']);

        $s3->complete($session['key'], $validated['upload_id'], $parts);

        $size = 0;
        try {
            $size = $s3->size($session['key']);
        } catch (\Throwable) {
            $size = 0;
        }

        $mediaFile = DB::transaction(function () use ($session, $validated, $size): MediaFile {
            $mediaFile = MediaFile::create([
                'organization_id' => $session['organization_id'],
                'folder_id' => $session['folder_id'],
                'title' => $validated['title'],
                'file_name' => $session['file_name'],
                'file_path' => $session['key'],
                'size' => $size,
                'status' => MediaFileStatus::Uploaded,
                'tags' => $validated['tags'] ?? [],
            ]);

            $mediaFile->profiles()->create([
                'profile_id' => $session['profile_id'],
                'name' => $session['profile_name'],
                'qualities' => $session['profile_qualities'],
            ]);

            return $mediaFile;
        });

        $this->dispatchTranscodeJob($mediaFile);

        Cache::forget($this->uploadCacheKey($validated['upload_id']));

        return response()->json(['ok' => true]);
    }

    public function abortMultipartUpload(Request $request, S3MultipartUploadManager $s3): JsonResponse
    {
        $validated = $request->validate([
            'upload_id' => ['required', 'string'],
        ]);

        $session = $this->authorizeUploadSession($request, $validated['upload_id']);

        try {
            $s3->abort($session['key'], $validated['upload_id']);
        } catch (\Throwable) {
            // best effort
        }

        Cache::forget($this->uploadCacheKey($validated['upload_id']));

        return response()->json(['ok' => true]);
    }

    private function resolveProfile(string $organizationId, string $profileId): Profile
    {
        return Profile::query()
            ->where('organization_id', $organizationId)
            ->findOr($profileId, fn () => abort(422, 'Selected profile is not available.'));
    }

    private function attachProfileSnapshot(MediaFile $mediaFile, Profile $profile): void
    {
        $mediaFile->profiles()->create([
            'profile_id' => $profile->id,
            'name' => $profile->name,
            'qualities' => $profile->qualities,
        ]);
    }

    /**
     * @return array{organization_id: string, user_id: int|string|null, folder_id: ?string, key: string, file_name: string, profile_id: string, profile_name: string, profile_qualities: array<int, string>}
     */
    private function authorizeUploadSession(Request $request, string $uploadId): array
    {
        /** @var array{organization_id: string, user_id: int|string|null, folder_id: ?string, key: string, file_name: string, profile_id: string, profile_name: string, profile_qualities: array<int, string>}|null $session */
        $session = Cache::get($this->uploadCacheKey($uploadId));

        abort_if($session === null, 404, 'Upload session not found.');

        abort_unless(
            $session['organization_id'] === $this->currentOrganizationId($request)
            && $session['user_id'] === $request->user()?->getKey(),
            403,
        );

        return $session;
    }

    private function uploadCacheKey(string $uploadId): string
    {
        return 'manage:upload:'.$uploadId;
    }

    private function currentOrganizationId(Request $request): string
    {
        $id = $request->session()->get('current_organization_id');

        abort_if($id === null, 403, 'No active organization.');

        return (string) $id;
    }

    private function dispatchTranscodeJob(MediaFile $mediaFile): void
    {
        Redis::lpush('queues:transcode', json_encode([
            'id' => $mediaFile->id,
            'organization_id' => $mediaFile->organization_id,
            'folder_id' => $mediaFile->folder_id,
            'title' => $mediaFile->title,
            'file_name' => $mediaFile->file_name,
            'file_path' => $mediaFile->file_path,
            'source_url' => $mediaFile->source_url,
            'streaming_url' => $mediaFile->streaming_url,
            'size' => $mediaFile->size,
            'status' => $mediaFile->status->value,
            'progress' => $mediaFile->progress,
            'created_at' => $mediaFile->created_at?->toIso8601String(),
            'updated_at' => $mediaFile->updated_at?->toIso8601String(),
        ]));
    }
}
