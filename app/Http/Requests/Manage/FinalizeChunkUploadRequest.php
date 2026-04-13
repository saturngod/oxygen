<?php

namespace App\Http\Requests\Manage;

use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;

class FinalizeChunkUploadRequest extends FormRequest
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
            'upload_id' => ['required', 'string', 'regex:/^[a-zA-Z0-9\-]{8,64}$/'],
            'total_chunks' => ['required', 'integer', 'min:1', 'max:20000'],
            'file_name' => ['required', 'string', 'max:255'],
            'title' => ['required', 'string', 'max:255'],
            'folder_id' => ['nullable', 'uuid', 'exists:folders,id'],
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
