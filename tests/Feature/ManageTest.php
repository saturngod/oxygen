<?php

use App\Enums\MediaFileStatus;
use App\Enums\OrganizationRole;
use App\Models\Folder;
use App\Models\MediaFile;
use App\Models\Organization;
use App\Models\User;
use App\Services\S3MultipartUploadManager;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Storage;

uses(RefreshDatabase::class);

function manageActor(): array
{
    $user = User::factory()->create();
    $organization = Organization::factory()->create();
    $organization->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    return [$user, $organization];
}

test('index lists folders and root-level files', function () {
    [$user, $org] = manageActor();

    $rootFile = MediaFile::factory()->for($org)->create(['title' => 'Root clip']);
    $folder = Folder::factory()->for($org)->create(['name' => 'Campaigns']);
    MediaFile::factory()->for($org)->create(['folder_id' => $folder->id, 'title' => 'Inside']);

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->get('/manage')
        ->assertInertia(fn ($page) => $page
            ->component('manage')
            ->where('currentFolder', null)
            ->has('folders', 1)
            ->has('files', 1)
            ->where('files.0.title', 'Root clip')
        );

    expect($rootFile->fresh())->not->toBeNull();
});

test('index scopes files to the selected folder', function () {
    [$user, $org] = manageActor();
    $folder = Folder::factory()->for($org)->create(['name' => 'Campaigns']);
    MediaFile::factory()->for($org)->create(['folder_id' => $folder->id, 'title' => 'Inside']);
    MediaFile::factory()->for($org)->create(['title' => 'Root clip']);

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->get('/manage?folder='.$folder->id)
        ->assertInertia(fn ($page) => $page
            ->where('currentFolder.id', $folder->id)
            ->has('files', 1)
            ->where('files.0.title', 'Inside')
        );
});

test('authenticated user can create a folder in active organization', function () {
    [$user, $org] = manageActor();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/folders', ['name' => 'Promo'])
        ->assertRedirect();

    expect(Folder::where('organization_id', $org->id)->where('name', 'Promo')->exists())->toBeTrue();
});

test('folder creation requires a name', function () {
    [$user, $org] = manageActor();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/folders', [])
        ->assertSessionHasErrors('name');
});

test('authenticated user can add a video from a URL', function () {
    [$user, $org] = manageActor();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files/url', [
            'title' => 'Remote clip',
            'source_url' => 'https://cdn.example.com/videos/promo.mp4',
            'tags' => ['remote'],
        ])
        ->assertRedirect();

    $media = MediaFile::where('organization_id', $org->id)->first();
    expect($media)->not->toBeNull();
    expect($media->source_url)->toBe('https://cdn.example.com/videos/promo.mp4');
    expect($media->file_name)->toBe('promo.mp4');
    expect($media->file_path)->toBeNull();
    expect($media->tags)->toBe(['remote']);
});

test('url import requires a valid url', function () {
    [$user, $org] = manageActor();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files/url', [
            'title' => 'Bad',
            'source_url' => 'not-a-url',
        ])
        ->assertSessionHasErrors('source_url');
});

test('init multipart upload returns upload id and caches session', function () {
    [$user, $org] = manageActor();

    $this->mock(S3MultipartUploadManager::class)
        ->shouldReceive('initiate')
        ->once()
        ->withArgs(function (string $key, string $contentType) {
            return str_ends_with($key, '.mp4') && $contentType === 'video/mp4';
        })
        ->andReturn('aws-upload-id');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->postJson('/manage/files/multipart/init', [
            'file_name' => 'promo.mp4',
        ])
        ->assertOk()
        ->assertJsonPath('upload_id', 'aws-upload-id');

    $session = Cache::get('manage:upload:aws-upload-id');
    expect($session)->not->toBeNull();
    expect($session['organization_id'])->toBe($org->getKey());
    expect($session['user_id'])->toBe($user->getKey());
});

test('init rejects unsupported extensions', function () {
    [$user, $org] = manageActor();

    $this->mock(S3MultipartUploadManager::class)
        ->shouldNotReceive('initiate');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->postJson('/manage/files/multipart/init', [
            'file_name' => 'doc.pdf',
        ])
        ->assertStatus(422);
});

test('sign part returns presigned url for owned session', function () {
    [$user, $org] = manageActor();

    Cache::put('manage:upload:aws-upload-id', [
        'organization_id' => $org->getKey(),
        'user_id' => $user->getKey(),
        'folder_id' => null,
        'key' => 'media/key.mp4',
        'file_name' => 'promo.mp4',
    ], now()->addHour());

    $this->mock(S3MultipartUploadManager::class)
        ->shouldReceive('presignPart')
        ->once()
        ->with('media/key.mp4', 'aws-upload-id', 3)
        ->andReturn('http://localhost:9000/signed');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->postJson('/manage/files/multipart/sign-part', [
            'upload_id' => 'aws-upload-id',
            'part_number' => 3,
        ])
        ->assertOk()
        ->assertJsonPath('url', 'http://localhost:9000/signed');
});

test('sign part rejects session owned by another user', function () {
    [$user, $org] = manageActor();
    $other = User::factory()->create();

    Cache::put('manage:upload:aws-upload-id', [
        'organization_id' => $org->getKey(),
        'user_id' => $other->getKey(),
        'folder_id' => null,
        'key' => 'media/key.mp4',
        'file_name' => 'promo.mp4',
    ], now()->addHour());

    $this->mock(S3MultipartUploadManager::class)->shouldNotReceive('presignPart');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->postJson('/manage/files/multipart/sign-part', [
            'upload_id' => 'aws-upload-id',
            'part_number' => 1,
        ])
        ->assertForbidden();
});

test('complete creates media file and clears cache', function () {
    [$user, $org] = manageActor();

    Cache::put('manage:upload:aws-upload-id', [
        'organization_id' => $org->getKey(),
        'user_id' => $user->getKey(),
        'folder_id' => null,
        'key' => 'media/'.$org->id.'/abc.mp4',
        'file_name' => 'promo.mp4',
    ], now()->addHour());

    $mock = $this->mock(S3MultipartUploadManager::class);
    $mock->shouldReceive('complete')
        ->once()
        ->with('media/'.$org->id.'/abc.mp4', 'aws-upload-id', [
            ['PartNumber' => 1, 'ETag' => '"etag-1"'],
            ['PartNumber' => 2, 'ETag' => '"etag-2"'],
        ]);
    $mock->shouldReceive('size')->once()->andReturn(12345);

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->postJson('/manage/files/multipart/complete', [
            'upload_id' => 'aws-upload-id',
            'title' => 'Big Promo',
            'parts' => [
                ['part_number' => 1, 'etag' => '"etag-1"'],
                ['part_number' => 2, 'etag' => '"etag-2"'],
            ],
            'tags' => ['launch'],
        ])
        ->assertOk()
        ->assertJsonPath('ok', true);

    $media = MediaFile::where('organization_id', $org->id)->first();
    expect($media)->not->toBeNull();
    expect($media->file_name)->toBe('promo.mp4');
    expect($media->file_path)->toBe('media/'.$org->id.'/abc.mp4');
    expect($media->size)->toBe(12345);
    expect($media->tags)->toBe(['launch']);
    expect(Cache::has('manage:upload:aws-upload-id'))->toBeFalse();
});

test('abort clears cache and calls s3 abort', function () {
    [$user, $org] = manageActor();

    Cache::put('manage:upload:aws-upload-id', [
        'organization_id' => $org->getKey(),
        'user_id' => $user->getKey(),
        'folder_id' => null,
        'key' => 'media/key.mp4',
        'file_name' => 'promo.mp4',
    ], now()->addHour());

    $this->mock(S3MultipartUploadManager::class)
        ->shouldReceive('abort')
        ->once()
        ->with('media/key.mp4', 'aws-upload-id');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->postJson('/manage/files/multipart/abort', [
            'upload_id' => 'aws-upload-id',
        ])
        ->assertOk();

    expect(Cache::has('manage:upload:aws-upload-id'))->toBeFalse();
});

test('authenticated user can delete a media file and its s3 object', function () {
    Storage::fake('s3');
    [$user, $org] = manageActor();

    Storage::disk('s3')->put('media/'.$org->id.'/clip.mp4', 'fake-bytes');

    $media = MediaFile::factory()->for($org)->create([
        'file_path' => 'media/'.$org->id.'/clip.mp4',
        'status' => MediaFileStatus::Uploaded,
    ]);

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->delete('/manage/files/'.$media->id)
        ->assertRedirect();

    expect(MediaFile::find($media->id))->toBeNull();
    Storage::disk('s3')->assertMissing('media/'.$org->id.'/clip.mp4');
});

test('deleting a file in progress is rejected', function () {
    Storage::fake('s3');
    [$user, $org] = manageActor();

    $media = MediaFile::factory()->for($org)->create([
        'status' => MediaFileStatus::Progress,
    ]);

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->delete('/manage/files/'.$media->id)
        ->assertStatus(422);

    expect(MediaFile::find($media->id))->not->toBeNull();
});

test('user cannot delete a file from another organization', function () {
    [$user, $org] = manageActor();
    $otherOrg = Organization::factory()->create();
    $media = MediaFile::factory()->for($otherOrg)->create();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->delete('/manage/files/'.$media->id)
        ->assertForbidden();

    expect(MediaFile::find($media->id))->not->toBeNull();
});

test('manage endpoints require an active organization', function () {
    $user = User::factory()->create();

    $this->actingAs($user)
        ->get('/manage')
        ->assertForbidden();
});
