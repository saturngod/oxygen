<?php

namespace App\Policies;

use App\Enums\OrganizationRole;
use App\Models\Organization;
use App\Models\User;

class OrganizationPolicy
{
    public function manage(User $user, Organization $organization): bool
    {
        return $user->hasOrganizationRole($organization, OrganizationRole::Admin);
    }
}
