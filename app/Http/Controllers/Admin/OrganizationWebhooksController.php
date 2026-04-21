<?php

namespace App\Http\Controllers\Admin;

use App\Enums\WebhookEvent;
use App\Http\Controllers\Controller;
use App\Http\Requests\Admin\StoreWebhookRequest;
use App\Http\Requests\Admin\UpdateWebhookRequest;
use App\Models\Organization;
use App\Models\Webhook;
use Illuminate\Http\RedirectResponse;
use Inertia\Inertia;
use Inertia\Response;

class OrganizationWebhooksController extends Controller
{
    public function index(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        return Inertia::render('admin/webhooks/index', [
            'organization' => [
                'id' => $organization->id,
                'name' => $organization->name,
            ],
            'webhooks' => $organization->webhooks()
                ->orderByDesc('created_at')
                ->get()
                ->map(fn (Webhook $webhook): array => [
                    'id' => $webhook->id,
                    'url' => $webhook->url,
                    'events' => $webhook->events,
                    'is_active' => $webhook->is_active,
                    'created_at' => $webhook->created_at->toIso8601String(),
                ])
                ->all(),
            'availableEvents' => collect(WebhookEvent::cases())
                ->map(fn (WebhookEvent $event): array => [
                    'value' => $event->value,
                    'label' => $event->label(),
                ])
                ->all(),
        ]);
    }

    public function store(StoreWebhookRequest $request, Organization $organization): RedirectResponse
    {
        $this->authorize('manage', $organization);

        $organization->webhooks()->create($request->validated());

        return to_route('admin.organizations.webhooks.index', $organization)
            ->with('toast', ['type' => 'success', 'message' => __('Webhook created.')]);
    }

    public function update(UpdateWebhookRequest $request, Organization $organization, Webhook $webhook): RedirectResponse
    {
        $this->authorize('manage', $organization);

        abort_unless($webhook->organization_id === $organization->id, 404);

        $webhook->update($request->validated());

        return to_route('admin.organizations.webhooks.index', $organization)
            ->with('toast', ['type' => 'success', 'message' => __('Webhook updated.')]);
    }

    public function destroy(Organization $organization, Webhook $webhook): RedirectResponse
    {
        $this->authorize('manage', $organization);

        abort_unless($webhook->organization_id === $organization->id, 404);

        $webhook->delete();

        return to_route('admin.organizations.webhooks.index', $organization)
            ->with('toast', ['type' => 'success', 'message' => __('Webhook deleted.')]);
    }
}
