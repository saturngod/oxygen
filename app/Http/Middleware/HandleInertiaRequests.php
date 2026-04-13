<?php

namespace App\Http\Middleware;

use Illuminate\Http\Request;
use Inertia\Middleware;

class HandleInertiaRequests extends Middleware
{
    /**
     * The root template that's loaded on the first page visit.
     *
     * @see https://inertiajs.com/server-side-setup#root-template
     *
     * @var string
     */
    protected $rootView = 'app';

    /**
     * Determines the current asset version.
     *
     * @see https://inertiajs.com/asset-versioning
     */
    public function version(Request $request): ?string
    {
        return parent::version($request);
    }

    /**
     * Define the props that are shared by default.
     *
     * @see https://inertiajs.com/shared-data
     *
     * @return array<string, mixed>
     */
    public function share(Request $request): array
    {
        $user = $request->user();
        $userPayload = null;
        $organizations = [];

        if ($user !== null) {
            $memberships = $user->organizations()->orderBy('name')->get();

            $currentId = $request->session()->get('current_organization_id');
            $current = $memberships->firstWhere('id', $currentId) ?? $memberships->first();

            if ($current !== null && $current->getKey() !== $currentId) {
                $request->session()->put('current_organization_id', $current->getKey());
            }

            $userPayload = array_merge($user->toArray(), [
                'current_role' => $current?->pivot->role,
                'current_organization' => $current === null ? null : [
                    'id' => $current->id,
                    'name' => $current->name,
                    'slug' => $current->slug,
                    'image_url' => $current->imageUrl(),
                ],
            ]);

            $organizations = $memberships->map(fn ($org) => [
                'id' => $org->id,
                'name' => $org->name,
                'slug' => $org->slug,
                'role' => $org->pivot->role,
                'image_url' => $org->imageUrl(),
            ])->all();
        }

        return [
            ...parent::share($request),
            'name' => config('app.name'),
            'auth' => [
                'user' => $userPayload,
                'organizations' => $organizations,
            ],
            'sidebarOpen' => ! $request->hasCookie('sidebar_state') || $request->cookie('sidebar_state') === 'true',
        ];
    }
}
