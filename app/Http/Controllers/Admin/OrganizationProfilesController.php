<?php

namespace App\Http\Controllers\Admin;

use App\Enums\VideoQuality;
use App\Http\Controllers\Controller;
use App\Http\Requests\Admin\StoreProfileRequest;
use App\Models\Organization;
use App\Models\Profile;
use Illuminate\Http\RedirectResponse;
use Illuminate\Support\Facades\DB;
use Inertia\Inertia;
use Inertia\Response;

class OrganizationProfilesController extends Controller
{
    public function index(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        return Inertia::render('admin/profiles/index', [
            'organization' => [
                'id' => $organization->id,
                'name' => $organization->name,
            ],
            'profiles' => $organization->profiles()
                ->orderByDesc('is_default')
                ->orderBy('name')
                ->get()
                ->map(fn (Profile $profile): array => [
                    'id' => $profile->id,
                    'name' => $profile->name,
                    'qualities' => $profile->qualities,
                    'is_default' => $profile->is_default,
                    'created_at' => $profile->created_at->toIso8601String(),
                ])
                ->all(),
        ]);
    }

    public function create(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        return Inertia::render('admin/profiles/create', [
            'organization' => [
                'id' => $organization->id,
                'name' => $organization->name,
            ],
            'qualities' => VideoQuality::catalog(),
        ]);
    }

    public function store(StoreProfileRequest $request, Organization $organization): RedirectResponse
    {
        $this->authorize('manage', $organization);

        $validated = $request->validated();
        $hasDefault = $organization->profiles()->where('is_default', true)->exists();
        $validated['is_default'] = ! $hasDefault;

        $organization->profiles()->create($validated);

        return to_route('admin.organizations.profiles.index', $organization)
            ->with('toast', ['type' => 'success', 'message' => __('Profile created.')]);
    }

    public function makeDefault(Organization $organization, Profile $profile): RedirectResponse
    {
        $this->authorize('manage', $organization);

        abort_unless($profile->organization_id === $organization->id, 404);

        DB::transaction(function () use ($organization, $profile): void {
            $organization->profiles()
                ->whereKeyNot($profile->id)
                ->where('is_default', true)
                ->update(['is_default' => false]);

            $profile->forceFill(['is_default' => true])->save();
        });

        return to_route('admin.organizations.profiles.index', $organization)
            ->with('toast', ['type' => 'success', 'message' => __('Default profile updated.')]);
    }
}
