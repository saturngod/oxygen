<?php

use App\Enums\OrganizationRole;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('guests are redirected to the login page', function () {
    $response = $this->get(route('manage'));
    $response->assertRedirect(route('login'));
});

test('authenticated organization members can visit manage', function () {
    $user = User::factory()->create();
    $organization = Organization::factory()->create();
    $organization->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $this->actingAs($user)
        ->get(route('manage'))
        ->assertOk();
});

test('authenticated users without an organization are forbidden from manage', function () {
    $user = User::factory()->create();

    $this->actingAs($user)
        ->get(route('manage'))
        ->assertForbidden();
});
