<?php

use App\Enums\OrganizationRole;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\UploadedFile;
use Illuminate\Support\Facades\Storage;

uses(RefreshDatabase::class);

test('admin can view organization settings page', function () {
    Storage::fake('public');

    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.settings.edit', $org))
        ->assertSuccessful();
});

test('operator cannot view organization settings page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.settings.edit', $org))
        ->assertForbidden();
});

test('non-member cannot view organization settings page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $this->actingAs($user)
        ->get(route('admin.organizations.settings.edit', $org))
        ->assertForbidden();
});

test('admin can update organization name', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create(['name' => 'Old Name']);

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => 'New Name',
        ])
        ->assertRedirect(route('admin.organizations.settings.edit', $org));

    expect($org->fresh()->name)->toBe('New Name');
});

test('admin can update organization contact details', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => 'Updated Org',
            'contact_email' => 'contact@example.com',
            'phone' => '+1 555-0100',
            'address' => '123 Main St',
        ])
        ->assertRedirect(route('admin.organizations.settings.edit', $org));

    $org = $org->fresh();
    expect($org->contact_email)->toBe('contact@example.com')
        ->and($org->phone)->toBe('+1 555-0100')
        ->and($org->address)->toBe('123 Main St');
});

test('admin can upload organization image', function () {
    Storage::fake('public');

    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $file = UploadedFile::fake()->image('logo.png');

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => $org->name,
            'image' => $file,
        ])
        ->assertRedirect(route('admin.organizations.settings.edit', $org));

    $org = $org->fresh();
    expect($org->image)->not->toBeNull();
    Storage::disk('public')->assertExists($org->image);
});

test('admin can replace organization image', function () {
    Storage::fake('public');

    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $firstFile = UploadedFile::fake()->image('logo1.png');
    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => $org->name,
            'image' => $firstFile,
        ]);

    $oldImage = $org->fresh()->image;

    $secondFile = UploadedFile::fake()->image('logo2.png');
    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => $org->name,
            'image' => $secondFile,
        ]);

    $org = $org->fresh();
    expect($org->image)->not->toBe($oldImage);
    Storage::disk('public')->assertExists($org->image);
});

test('name is required when updating organization settings', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => '',
        ])
        ->assertSessionHasErrors('name');
});

test('contact email must be valid', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => $org->name,
            'contact_email' => 'not-an-email',
        ])
        ->assertSessionHasErrors('contact_email');
});

test('image must be a valid image file', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $badFile = UploadedFile::fake()->create('document.pdf', 100);

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => $org->name,
            'image' => $badFile,
        ])
        ->assertSessionHasErrors('image');
});

test('operator cannot update organization settings', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create(['name' => 'Original']);

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.settings.update', $org), [
            'name' => 'Changed',
        ])
        ->assertForbidden();

    expect($org->fresh()->name)->toBe('Original');
});
