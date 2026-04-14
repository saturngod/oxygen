<?php

use App\Enums\OrganizationRole;
use App\Enums\VideoQuality;
use App\Models\Organization;
use App\Models\Profile;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('admin can view organization profiles page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.profiles.index', $org))
        ->assertSuccessful();
});

test('operator cannot view organization profiles page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.profiles.index', $org))
        ->assertForbidden();
});

test('non-member cannot view organization profiles page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $this->actingAs($user)
        ->get(route('admin.organizations.profiles.index', $org))
        ->assertForbidden();
});

test('admin can view profile create page with quality catalog', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.profiles.create', $org))
        ->assertSuccessful();
});

test('admin can create a profile with selected qualities', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $payload = [
        'name' => 'Standard Web Delivery',
        'qualities' => [
            VideoQuality::Sd480p->value,
            VideoQuality::Hd720p->value,
            VideoQuality::Hd1080p->value,
        ],
    ];

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), $payload)
        ->assertRedirect(route('admin.organizations.profiles.index', $org));

    $profile = Profile::query()->where('organization_id', $org->id)->sole();

    expect($profile->name)->toBe('Standard Web Delivery')
        ->and($profile->qualities)->toEqualCanonicalizing($payload['qualities']);
});

test('profile requires at least one quality', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->from(route('admin.organizations.profiles.create', $org))
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Empty Profile',
            'qualities' => [],
        ])
        ->assertSessionHasErrors('qualities');

    expect(Profile::query()->count())->toBe(0);
});

test('profile rejects unknown quality values', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->from(route('admin.organizations.profiles.create', $org))
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Bad Profile',
            'qualities' => ['9001p'],
        ])
        ->assertSessionHasErrors('qualities.0');
});

test('first profile is automatically marked as default', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Primary',
            'qualities' => [VideoQuality::Hd720p->value],
        ])
        ->assertRedirect();

    expect(Profile::query()->where('organization_id', $org->id)->sole()->is_default)->toBeTrue();
});

test('additional profiles are not default when one already exists', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    Profile::factory()->for($org)->create(['name' => 'First', 'is_default' => true]);

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Second',
            'qualities' => [VideoQuality::Hd720p->value],
        ])
        ->assertRedirect();

    $second = Profile::query()->where('name', 'Second')->sole();
    expect($second->is_default)->toBeFalse();
});

test('admin can promote another profile to default', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $current = Profile::factory()->for($org)->create(['is_default' => true]);
    $target = Profile::factory()->for($org)->create(['is_default' => false]);

    $this->actingAs($user)
        ->put(route('admin.organizations.profiles.default', [$org, $target]))
        ->assertRedirect(route('admin.organizations.profiles.index', $org));

    expect($target->refresh()->is_default)->toBeTrue()
        ->and($current->refresh()->is_default)->toBeFalse();
});

test('make default is scoped to the organization', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();
    $otherOrg = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $foreign = Profile::factory()->for($otherOrg)->create(['is_default' => false]);

    $this->actingAs($user)
        ->put(route('admin.organizations.profiles.default', [$org, $foreign]))
        ->assertNotFound();

    expect($foreign->refresh()->is_default)->toBeFalse();
});

test('operator cannot make a profile default', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $profile = Profile::factory()->for($org)->create(['is_default' => false]);

    $this->actingAs($user)
        ->put(route('admin.organizations.profiles.default', [$org, $profile]))
        ->assertForbidden();

    expect($profile->refresh()->is_default)->toBeFalse();
});

test('operator cannot create a profile', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Unauthorized',
            'qualities' => [VideoQuality::Hd720p->value],
        ])
        ->assertForbidden();
});
