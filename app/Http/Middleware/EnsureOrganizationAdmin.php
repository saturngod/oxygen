<?php

namespace App\Http\Middleware;

use App\Enums\OrganizationRole;
use App\Models\Organization;
use Closure;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\Response;

class EnsureOrganizationAdmin
{
    /**
     * Handle an incoming request.
     *
     * @param  Closure(Request): (Response)  $next
     */
    public function handle(Request $request, Closure $next): Response
    {
        $organization = $request->route('organization');

        if (! $organization instanceof Organization) {
            abort(404);
        }

        if (! $request->user()->hasOrganizationRole($organization, OrganizationRole::Admin)) {
            abort(403);
        }

        return $next($request);
    }
}
