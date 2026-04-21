<?php

namespace App\Http\Requests\Admin;

use App\Enums\VideoQuality;
use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class UpdateProfileRequest extends FormRequest
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
            'name' => ['required', 'string', 'max:255'],
            'qualities' => ['required', 'array', 'min:1'],
            'qualities.*' => ['required', 'string', Rule::enum(VideoQuality::class)],
        ];
    }
}
