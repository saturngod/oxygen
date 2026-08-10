<?php

namespace App\Http\Controllers\Admin;

use App\Enums\LiveStreamViewerPeriod;
use App\Http\Controllers\Controller;
use App\Http\Requests\Admin\ShowLiveStreamViewerRequest;
use App\Models\LiveStream;
use App\Models\Organization;
use App\Services\LiveStreamViewerAnalytics;
use Inertia\Inertia;
use Inertia\Response;

class OrganizationLiveStreamViewerController extends Controller
{
    public function __invoke(
        ShowLiveStreamViewerRequest $request,
        Organization $organization,
        LiveStream $liveStream,
        LiveStreamViewerAnalytics $analytics,
    ): Response {
        $this->authorize('manage', $organization);
        abort_unless($liveStream->organization_id === $organization->id, 404);

        $period = $request->enum('period', LiveStreamViewerPeriod::class)
            ?? LiveStreamViewerPeriod::Day;

        return Inertia::render('admin/live-streams/viewer', [
            'organization' => [
                'id' => $organization->id,
                'name' => $organization->name,
            ],
            'liveStream' => [
                'id' => $liveStream->id,
                'title' => $liveStream->title,
                'status' => $liveStream->status->value,
                'status_label' => $liveStream->status->label(),
            ],
            'period' => $period->value,
            'analytics' => $analytics->build($liveStream, $period),
        ]);
    }
}
