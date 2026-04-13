<?php

namespace App\Http\Controllers;

use App\Models\Organization;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;

class OrganizationSwitchController extends Controller
{
    public function __invoke(Request $request, Organization $organization): RedirectResponse
    {
        abort_unless(
            $request->user()->organizations()->whereKey($organization->getKey())->exists(),
            403,
        );

        $request->session()->put('current_organization_id', $organization->getKey());

        return back();
    }
}
