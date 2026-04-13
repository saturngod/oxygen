<?php

use App\Enums\OrganizationRole;
use App\Models\Folder;
use App\Models\MediaFile;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\UploadedFile;
use Illuminate\Support\Facades\Storage;
use Illuminate\Support\Str;

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

test('authenticated user can upload an mp4 file', function () {
    Storage::fake('s3');
    [$user, $org] = manageActor();

    $file = UploadedFile::fake()->create('promo.mp4', 1024, 'video/mp4');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files', [
            'title' => 'Promo Spot',
            'folder_id' => null,
            'tags' => ['summer', 'launch'],
            'file' => $file,
        ])
        ->assertRedirect();

    $media = MediaFile::where('organization_id', $org->id)->first();
    expect($media)->not->toBeNull();
    expect($media->title)->toBe('Promo Spot');
    expect($media->tags)->toBe(['summer', 'launch']);
    expect($media->status->value)->toBe('uploaded');
    Storage::disk('s3')->assertExists($media->file_path);
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

test('upload rejects non-video files', function () {
    Storage::fake('s3');
    [$user, $org] = manageActor();

    $file = UploadedFile::fake()->create('doc.pdf', 100, 'application/pdf');

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files', [
            'title' => 'Bad',
            'file' => $file,
        ])
        ->assertSessionHasErrors('file');
});

test('chunked upload assembles chunks and creates a media file', function () {
    Storage::fake('local');
    Storage::fake('s3');
    [$user, $org] = manageActor();

    $uploadId = (string) Str::uuid();
    $chunks = ['AAAA', 'BBBB', 'CCCC'];

    foreach ($chunks as $index => $contents) {
        $chunk = UploadedFile::fake()->createWithContent("{$index}", $contents);

        $this->actingAs($user)
            ->withSession(['current_organization_id' => $org->getKey()])
            ->post('/manage/files/chunk', [
                'upload_id' => $uploadId,
                'chunk_index' => $index,
                'total_chunks' => count($chunks),
                'chunk' => $chunk,
            ])
            ->assertOk()
            ->assertJsonPath('upload_id', $uploadId);
    }

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files/chunk/finalize', [
            'upload_id' => $uploadId,
            'total_chunks' => count($chunks),
            'file_name' => 'promo.mp4',
            'title' => 'Big Promo',
            'tags' => ['launch'],
        ])
        ->assertRedirect();

    $media = MediaFile::where('organization_id', $org->id)->first();
    expect($media)->not->toBeNull();
    expect($media->file_name)->toBe('promo.mp4');
    expect($media->size)->toBe(strlen(implode('', $chunks)));
    Storage::disk('s3')->assertExists($media->file_path);
    expect(Storage::disk('s3')->get($media->file_path))->toBe(implode('', $chunks));
    Storage::disk('local')->assertMissing("chunks/{$org->id}/{$uploadId}");
});

test('chunked finalize fails when a chunk is missing', function () {
    Storage::fake('local');
    Storage::fake('s3');
    [$user, $org] = manageActor();

    $uploadId = (string) Str::uuid();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files/chunk', [
            'upload_id' => $uploadId,
            'chunk_index' => 0,
            'total_chunks' => 2,
            'chunk' => UploadedFile::fake()->createWithContent('0', 'AAAA'),
        ])
        ->assertOk();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files/chunk/finalize', [
            'upload_id' => $uploadId,
            'total_chunks' => 2,
            'file_name' => 'promo.mp4',
            'title' => 'Incomplete',
        ])
        ->assertStatus(422);
});

test('chunk upload can be cancelled', function () {
    Storage::fake('local');
    [$user, $org] = manageActor();

    $uploadId = (string) Str::uuid();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->post('/manage/files/chunk', [
            'upload_id' => $uploadId,
            'chunk_index' => 0,
            'total_chunks' => 3,
            'chunk' => UploadedFile::fake()->createWithContent('0', 'AAAA'),
        ])
        ->assertOk();

    $this->actingAs($user)
        ->withSession(['current_organization_id' => $org->getKey()])
        ->delete("/manage/files/chunk/{$uploadId}")
        ->assertOk();

    Storage::disk('local')->assertMissing("chunks/{$org->id}/{$uploadId}");
});

test('manage endpoints require an active organization', function () {
    $user = User::factory()->create();

    $this->actingAs($user)
        ->get('/manage')
        ->assertForbidden();
});
