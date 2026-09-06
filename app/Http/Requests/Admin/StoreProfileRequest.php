<?php

namespace App\Http\Requests\Admin;

use App\Enums\VideoQuality;
use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class StoreProfileRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    /**
     * @return array<string, ValidationRule|array<mixed>|string>
     */
    protected function prepareForValidation(): void
    {
        // An unchecked quality list submits no key at all; treat that as an
        // explicit empty selection (live passthrough) instead of failing.
        if (! $this->exists('qualities')) {
            $this->merge(['qualities' => []]);
        }
    }

    /**
     * @return array<string, ValidationRule|array<mixed>|string>
     */
    public function rules(): array
    {
        return [
            'name' => ['required', 'string', 'max:255'],
            // An empty qualities list means live passthrough (direct RTMP->HLS
            // remux without transcoding). VOD uploads reject such profiles in
            // ManageController because the transcode worker needs renditions.
            'qualities' => ['present', 'array'],
            'qualities.*' => ['string', 'distinct', Rule::enum(VideoQuality::class)],
            'generate_thumbnail' => ['required', 'boolean'],
            'video_segment_duration_seconds' => ['required', 'integer', 'between:1,30'],
            'live_segment_duration_seconds' => ['required', 'integer', 'between:1,30'],
        ];
    }
}
