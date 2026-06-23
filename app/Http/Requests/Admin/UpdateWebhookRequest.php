<?php

namespace App\Http\Requests\Admin;

use App\Enums\WebhookEvent;
use App\Rules\PublicUrl;
use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class UpdateWebhookRequest extends FormRequest
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
            'url' => ['required', 'string', 'url', 'max:2048', new PublicUrl],
            'events' => ['required', 'array', 'min:1'],
            'events.*' => ['required', 'string', Rule::enum(WebhookEvent::class)],
            'is_active' => ['sometimes', 'boolean'],
        ];
    }
}
