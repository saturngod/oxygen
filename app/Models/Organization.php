<?php

namespace App\Models;

use App\Enums\OrganizationRole;
use Database\Factories\OrganizationFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Concerns\HasUuids;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsToMany;
use Illuminate\Support\Facades\Storage;

#[Fillable(['name', 'slug', 'image', 'contact_email', 'phone', 'address'])]
class Organization extends Model
{
    /** @use HasFactory<OrganizationFactory> */
    use HasFactory, HasUuids;

    public function users(): BelongsToMany
    {
        return $this->belongsToMany(User::class)
            ->withPivot('role')
            ->withTimestamps();
    }

    public function hasUserWithRole(User $user, OrganizationRole $role): bool
    {
        return $this->users()
            ->wherePivot('user_id', $user->getKey())
            ->wherePivot('role', $role->value)
            ->exists();
    }

    public function imageUrl(): string
    {
        return $this->image
            ? Storage::disk('public')->url($this->image)
            : '';
    }
}
