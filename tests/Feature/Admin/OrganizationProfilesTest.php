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
    Profile::factory()->for($org)->create([
        'video_segment_duration_seconds' => 8,
        'live_segment_duration_seconds' => 3,
    ]);

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.profiles.index', $org))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->where('profiles.0.video_segment_duration_seconds', 8)
            ->where('profiles.0.live_segment_duration_seconds', 3)
        );
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
        'generate_thumbnail' => true,
        'video_segment_duration_seconds' => 8,
        'live_segment_duration_seconds' => 3,
    ];

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), $payload)
        ->assertRedirect(route('admin.organizations.profiles.index', $org));

    $profile = Profile::query()->where('organization_id', $org->id)->sole();

    expect($profile->name)->toBe('Standard Web Delivery')
        ->and($profile->qualities)->toEqualCanonicalizing($payload['qualities'])
        ->and($profile->generate_thumbnail)->toBeTrue()
        ->and($profile->video_segment_duration_seconds)->toBe(8)
        ->and($profile->live_segment_duration_seconds)->toBe(3);
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
            'generate_thumbnail' => false,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
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
            'generate_thumbnail' => false,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
        ])
        ->assertSessionHasErrors('qualities.0');
});

test('profile rejects a non-boolean thumbnail option', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->from(route('admin.organizations.profiles.create', $org))
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Invalid Thumbnail Profile',
            'qualities' => [VideoQuality::Hd720p->value],
            'generate_thumbnail' => 'sometimes',
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
        ])
        ->assertSessionHasErrors('generate_thumbnail');

    expect(Profile::query()->count())->toBe(0);
});

test('profile segment durations must be whole seconds between 1 and 30', function (string $field, mixed $value) {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $payload = [
        'name' => 'Invalid Duration Profile',
        'qualities' => [VideoQuality::Hd720p->value],
        'generate_thumbnail' => false,
        'video_segment_duration_seconds' => 6,
        'live_segment_duration_seconds' => 2,
    ];
    $payload[$field] = $value;

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), $payload)
        ->assertSessionHasErrors($field);

    expect(Profile::query()->count())->toBe(0);
})->with([
    'video below minimum' => ['video_segment_duration_seconds', 0],
    'video above maximum' => ['video_segment_duration_seconds', 31],
    'live fractional' => ['live_segment_duration_seconds', 1.5],
]);

test('profile segment duration boundaries are accepted', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Boundary Duration Profile',
            'qualities' => [VideoQuality::Hd720p->value],
            'generate_thumbnail' => false,
            'video_segment_duration_seconds' => 1,
            'live_segment_duration_seconds' => 30,
        ])
        ->assertRedirect();

    $profile = Profile::query()->sole();
    expect($profile->video_segment_duration_seconds)->toBe(1)
        ->and($profile->live_segment_duration_seconds)->toBe(30);
});

test('first profile is automatically marked as default', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->post(route('admin.organizations.profiles.store', $org), [
            'name' => 'Primary',
            'qualities' => [VideoQuality::Hd720p->value],
            'generate_thumbnail' => false,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
        ])
        ->assertRedirect();

    $profile = Profile::query()->where('organization_id', $org->id)->sole();

    expect($profile->is_default)->toBeTrue()
        ->and($profile->generate_thumbnail)->toBeFalse();
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
            'generate_thumbnail' => false,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
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
            'generate_thumbnail' => false,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
        ])
        ->assertForbidden();
});

test('admin can view profile edit page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $profile = Profile::factory()->for($org)->create([
        'name' => 'Test Profile',
        'generate_thumbnail' => true,
        'video_segment_duration_seconds' => 6,
        'live_segment_duration_seconds' => 2,
    ]);

    $this->actingAs($user)
        ->get(route('admin.organizations.profiles.edit', [$org, $profile]))
        ->assertSuccessful()
        ->assertInertia(fn ($page) => $page
            ->where('profile.generate_thumbnail', true)
        );
});

test('admin can update a profile name and qualities', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $profile = Profile::factory()->for($org)->create([
        'name' => 'Old Name',
        'qualities' => [VideoQuality::Hd720p->value],
        'generate_thumbnail' => false,
        'video_segment_duration_seconds' => 6,
        'live_segment_duration_seconds' => 2,
    ]);

    $payload = [
        'name' => 'Updated Name',
        'qualities' => [VideoQuality::Hd720p->value, VideoQuality::Hd1080p->value],
        'generate_thumbnail' => true,
        'video_segment_duration_seconds' => 10,
        'live_segment_duration_seconds' => 4,
    ];

    $this->actingAs($user)
        ->put(route('admin.organizations.profiles.update', [$org, $profile]), $payload)
        ->assertRedirect(route('admin.organizations.profiles.index', $org));

    $profile->refresh();

    expect($profile->name)->toBe('Updated Name')
        ->and($profile->qualities)->toEqualCanonicalizing($payload['qualities'])
        ->and($profile->generate_thumbnail)->toBeTrue()
        ->and($profile->video_segment_duration_seconds)->toBe(10)
        ->and($profile->live_segment_duration_seconds)->toBe(4);
});

test('update is scoped to the organization', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();
    $otherOrg = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $foreign = Profile::factory()->for($otherOrg)->create(['name' => 'Foreign']);

    $this->actingAs($user)
        ->put(route('admin.organizations.profiles.update', [$org, $foreign]), [
            'name' => 'Hacked',
            'qualities' => [VideoQuality::Hd720p->value],
            'generate_thumbnail' => true,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
        ])
        ->assertNotFound();

    expect($foreign->refresh()->name)->toBe('Foreign');
});

test('operator cannot update a profile', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $profile = Profile::factory()->for($org)->create(['name' => 'Protected']);

    $this->actingAs($user)
        ->put(route('admin.organizations.profiles.update', [$org, $profile]), [
            'name' => 'Changed',
            'qualities' => [VideoQuality::Hd720p->value],
            'generate_thumbnail' => true,
            'video_segment_duration_seconds' => 6,
            'live_segment_duration_seconds' => 2,
        ])
        ->assertForbidden();

    expect($profile->refresh()->name)->toBe('Protected');
});
