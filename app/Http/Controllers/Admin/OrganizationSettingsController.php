<?php

namespace App\Http\Controllers\Admin;

use App\Http\Controllers\Controller;
use App\Http\Requests\Admin\UpdateOrganizationSettingsRequest;
use App\Models\Organization;
use Illuminate\Http\RedirectResponse;
use Illuminate\Support\Facades\Storage;
use Inertia\Inertia;
use Inertia\Response;

class OrganizationSettingsController extends Controller
{
    public function edit(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        return Inertia::render('admin/settings', [
            'organization' => [
                'id' => $organization->id,
                'name' => $organization->name,
                'image_url' => $organization->imageUrl(),
                'contact_email' => $organization->contact_email,
                'phone' => $organization->phone,
                'address' => $organization->address,
            ],
        ]);
    }

    public function update(UpdateOrganizationSettingsRequest $request, Organization $organization): RedirectResponse
    {
        $this->authorize('manage', $organization);

        $validated = $request->validated();

        if ($request->hasFile('image')) {
            if ($organization->image) {
                Storage::disk('public')->delete($organization->image);
            }

            $validated['image'] = $request->file('image')->store('organizations', 'public');
        }

        $organization->update($validated);

        return to_route('admin.organizations.settings.edit', $organization)->with('toast', ['type' => 'success', 'message' => __('Organization settings updated.')]);
    }
}
