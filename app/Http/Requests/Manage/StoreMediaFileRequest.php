<?php

namespace App\Http\Requests\Manage;

use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;

class StoreMediaFileRequest extends FormRequest
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
            'folder_id' => ['nullable', 'uuid', 'exists:folders,id'],
            'file' => ['required', 'file', 'mimetypes:video/mp4,video/quicktime', 'max:5120000'],
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
