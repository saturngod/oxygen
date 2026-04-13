<?php

use App\Enums\OrganizationRole;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('admin can view organization users page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.users.index', $org))
        ->assertSuccessful();
});

test('operator cannot view organization users page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $org->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->get(route('admin.organizations.users.index', $org))
        ->assertForbidden();
});

test('non-member cannot view organization users page', function () {
    $user = User::factory()->create(['email_verified_at' => now()]);
    $org = Organization::factory()->create();

    $this->actingAs($user)
        ->get(route('admin.organizations.users.index', $org))
        ->assertForbidden();
});
