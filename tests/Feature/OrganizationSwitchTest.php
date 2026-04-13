<?php

use App\Enums\OrganizationRole;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

test('authorized user can switch to another organization', function () {
    $user = User::factory()->create();
    $first = Organization::factory()->create();
    $second = Organization::factory()->create();

    $first->users()->attach($user, ['role' => OrganizationRole::Admin->value]);
    $second->users()->attach($user, ['role' => OrganizationRole::Operator->value]);

    $response = $this->actingAs($user)
        ->from('/dashboard')
        ->put(route('organizations.switch', $second));

    $response->assertRedirect('/dashboard');
    expect(session('current_organization_id'))->toBe($second->getKey());
});

test('user cannot switch to organization they do not belong to', function () {
    $user = User::factory()->create();
    $foreign = Organization::factory()->create();

    $this->actingAs($user)
        ->put(route('organizations.switch', $foreign))
        ->assertForbidden();

    expect(session('current_organization_id'))->toBeNull();
});
