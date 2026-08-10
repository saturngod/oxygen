<?php

namespace App\Http\Requests\Admin;

use App\Models\Organization;
use App\Models\Profile;
use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class StoreLiveStreamRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    /**
     * @return array<string, ValidationRule|array<mixed>|string>
     */
    public function rules(): array
    {
        $organization = $this->route('organization');

        return [
            'title' => ['required', 'string', 'max:255'],
            'profile_id' => [
                'required',
                'uuid',
                Rule::exists(Profile::class, 'id')->where(
                    'organization_id',
                    $organization instanceof Organization ? $organization->getKey() : null,
                ),
            ],
        ];
    }
}
