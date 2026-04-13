<?php

use App\Enums\OrganizationRole;
use App\Models\Organization;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Laravel\Fortify\Features;

uses(RefreshDatabase::class);

beforeEach(function () {
    $this->skipUnlessFortifyHas(Features::registration());
});

test('registration screen can be rendered', function () {
    $response = $this->get(route('register'));

    $response->assertOk();
});

test('new users can register and become organization admin', function () {
    $response = $this->post(route('register.store'), [
        'organization_name' => 'Acme Inc.',
        'name' => 'Test User',
        'email' => 'test@example.com',
        'password' => 'password',
        'password_confirmation' => 'password',
    ]);

    $this->assertAuthenticated();
    $response->assertRedirect(route('dashboard', absolute: false));

    $user = User::where('email', 'test@example.com')->first();
    $organization = Organization::where('name', 'Acme Inc.')->first();

    expect($organization)->not->toBeNull()
        ->and($organization->slug)->toBe('acme-inc')
        ->and($user->hasOrganizationRole($organization, OrganizationRole::Admin))->toBeTrue();
});

test('organization name is required when registering', function () {
    $response = $this->from(route('register'))->post(route('register.store'), [
        'organization_name' => '',
        'name' => 'Test User',
        'email' => 'test@example.com',
        'password' => 'password',
        'password_confirmation' => 'password',
    ]);

    $response->assertSessionHasErrors('organization_name');
    $this->assertGuest();
});
