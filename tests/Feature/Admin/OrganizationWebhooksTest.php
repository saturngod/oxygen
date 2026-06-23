<?php

use App\Enums\OrganizationRole;
use App\Enums\WebhookEvent;
use App\Models\Organization;
use App\Models\User;
use App\Models\Webhook;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

function webhookAdmin(): array
{
    $user = User::factory()->create(['email_verified_at' => now()]);
    $organization = Organization::factory()->create();

    $organization->users()->attach($user, ['role' => OrganizationRole::Admin->value]);

    return [$user, $organization];
}

test('admin can create a webhook with a public url', function () {
    [$user, $organization] = webhookAdmin();

    $this->actingAs($user)
        ->post(route('admin.organizations.webhooks.store', $organization), [
            'url' => 'https://hooks.example.com/incoming',
            'events' => [WebhookEvent::FileUploaded->value],
        ])
        ->assertRedirect(route('admin.organizations.webhooks.index', $organization));

    expect(Webhook::query()->where('organization_id', $organization->id)->count())->toBe(1);
});

test('webhook url cannot point to internal or metadata addresses', function () {
    [$user, $organization] = webhookAdmin();

    foreach ([
        'http://169.254.169.254/latest/meta-data/',
        'http://127.0.0.1:6379/',
        'http://10.1.2.3/hook',
    ] as $url) {
        $this->actingAs($user)
            ->post(route('admin.organizations.webhooks.store', $organization), [
                'url' => $url,
                'events' => [WebhookEvent::FileUploaded->value],
            ])
            ->assertSessionHasErrors('url');
    }

    expect(Webhook::query()->count())->toBe(0);
});
