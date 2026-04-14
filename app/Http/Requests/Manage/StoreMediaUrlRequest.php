<?php

namespace App\Http\Requests\Manage;

use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;

class StoreMediaUrlRequest extends FormRequest
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
        return [
            'title' => ['required', 'string', 'max:255'],
            'source_url' => ['required', 'url:http,https', 'max:2048'],
            'folder_id' => ['nullable', 'uuid', 'exists:folders,id'],
            'profile_id' => ['required', 'uuid'],
            'tags' => ['nullable', 'array'],
            'tags.*' => ['string', 'max:50'],
        ];
    }

    protected function prepareForValidation(): void
    {
        if ($this->has('tags') && is_string($this->input('tags'))) {
            $decoded = json_decode((string) $this->input('tags'), true);
            $this->merge(['tags' => is_array($decoded) ? $decoded : []]);
        }
    }
}
