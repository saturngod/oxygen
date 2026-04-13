<?php

use App\Enums\OrganizationRole;
use App\Models\Folder;
use App\Models\MediaFile;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\UploadedFile;
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

test('authenticated user can upload an mp4 file', function () {
    Storage::fake('public');
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
    Storage::disk('public')->assertExists($media->file_path);
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
    Storage::fake('public');
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

test('manage endpoints require an active organization', function () {
    $user = User::factory()->create();

    $this->actingAs($user)
        ->get('/manage')
        ->assertForbidden();
});
