<?php

namespace App\Http\Controllers\Admin;

use App\Http\Controllers\Controller;
use App\Models\Organization;
use Inertia\Inertia;
use Inertia\Response;

class OrganizationUsersController extends Controller
{
    public function index(Organization $organization): Response
    {
        $this->authorize('manage', $organization);

        return Inertia::render('admin/users', [
            'organization' => [
                'id' => $organization->id,
                'name' => $organization->name,
            ],
            'users' => $organization->users()->get()->map(fn ($user) => [
                'id' => $user->id,
                'name' => $user->name,
                'email' => $user->email,
                'role' => $user->pivot->role,
                'created_at' => $user->created_at->toIso8601String(),
            ]),
        ]);
    }
}
