<?php

use App\Http\Controllers\Admin\OrganizationLiveStreamsController;
use App\Http\Controllers\Admin\OrganizationProfilesController;
use App\Http\Controllers\Admin\OrganizationSettingsController;
use App\Http\Controllers\Admin\OrganizationUsersController;
use App\Http\Controllers\Admin\OrganizationWebhooksController;
use App\Http\Controllers\Internal\LiveStreamServiceController;
use App\Http\Controllers\ManageController;
use App\Http\Controllers\OrganizationSwitchController;
use App\Http\Controllers\StatusController;
use App\Http\Middleware\EnsureOrganizationAdmin;
use App\Http\Middleware\EnsureOrganizationMember;
use Illuminate\Support\Facades\Route;

Route::redirect('/', '/manage');

Route::prefix('internal/live')->group(function () {
    Route::post('auth-publish', [LiveStreamServiceController::class, 'authPublish'])
        ->name('internal.live.auth-publish');
    Route::post('session-started', [LiveStreamServiceController::class, 'sessionStarted'])
        ->name('internal.live.session-started');
    Route::post('session-ended', [LiveStreamServiceController::class, 'sessionEnded'])
        ->name('internal.live.session-ended');
    Route::post('session-failed', [LiveStreamServiceController::class, 'sessionFailed'])
        ->name('internal.live.session-failed');
    Route::post('recover-active', [LiveStreamServiceController::class, 'recoverActive'])
        ->name('internal.live.recover-active');
    Route::post('viewer-snapshot', [LiveStreamServiceController::class, 'viewerSnapshot'])
        ->name('internal.live.viewer-snapshot');
});

Route::middleware(['auth', 'verified'])->group(function () {
    Route::put('organizations/{organization}/switch', OrganizationSwitchController::class)
        ->name('organizations.switch');

    Route::middleware(EnsureOrganizationMember::class)->group(function () {
        Route::get('manage', [ManageController::class, 'index'])->name('manage');
        Route::post('manage/folders', [ManageController::class, 'storeFolder'])->name('manage.folders.store');
        Route::post('manage/files/url', [ManageController::class, 'storeFromUrl'])->name('manage.files.url');
        Route::delete('manage/files/{mediaFile}', [ManageController::class, 'destroyFile'])->name('manage.files.destroy');
        Route::post('manage/files/multipart/init', [ManageController::class, 'initMultipartUpload'])->name('manage.files.multipart.init');
        Route::post('manage/files/multipart/sign-part', [ManageController::class, 'signPart'])->name('manage.files.multipart.sign');
        Route::post('manage/files/multipart/complete', [ManageController::class, 'completeMultipartUpload'])->name('manage.files.multipart.complete');
        Route::post('manage/files/multipart/abort', [ManageController::class, 'abortMultipartUpload'])->name('manage.files.multipart.abort');

        Route::get('status', StatusController::class)->name('status');
    });

    Route::prefix('admin/organizations/{organization}')->middleware(EnsureOrganizationAdmin::class)->group(function () {
        Route::get('settings', [OrganizationSettingsController::class, 'edit'])->name('admin.organizations.settings.edit');
        Route::post('settings', [OrganizationSettingsController::class, 'update'])->name('admin.organizations.settings.update');
        Route::get('users', [OrganizationUsersController::class, 'index'])->name('admin.organizations.users.index');
        Route::get('live-streams', [OrganizationLiveStreamsController::class, 'index'])->name('admin.organizations.live-streams.index');
        Route::get('live-streams/create', [OrganizationLiveStreamsController::class, 'create'])->name('admin.organizations.live-streams.create');
        Route::post('live-streams', [OrganizationLiveStreamsController::class, 'store'])->name('admin.organizations.live-streams.store');
        Route::get('live-streams/{liveStream}', [OrganizationLiveStreamsController::class, 'show'])->name('admin.organizations.live-streams.show');
        Route::put('live-streams/{liveStream}', [OrganizationLiveStreamsController::class, 'update'])->name('admin.organizations.live-streams.update');
        Route::post('live-streams/{liveStream}/rotate-key', [OrganizationLiveStreamsController::class, 'rotateKey'])->name('admin.organizations.live-streams.rotate-key');
        Route::post('live-streams/{liveStream}/restart', [OrganizationLiveStreamsController::class, 'restart'])->name('admin.organizations.live-streams.restart');
        Route::post('live-streams/{liveStream}/disable', [OrganizationLiveStreamsController::class, 'disable'])->name('admin.organizations.live-streams.disable');
        Route::get('profiles', [OrganizationProfilesController::class, 'index'])->name('admin.organizations.profiles.index');
        Route::get('profiles/create', [OrganizationProfilesController::class, 'create'])->name('admin.organizations.profiles.create');
        Route::post('profiles', [OrganizationProfilesController::class, 'store'])->name('admin.organizations.profiles.store');
        Route::get('profiles/{profile}/edit', [OrganizationProfilesController::class, 'edit'])->name('admin.organizations.profiles.edit');
        Route::put('profiles/{profile}', [OrganizationProfilesController::class, 'update'])->name('admin.organizations.profiles.update');
        Route::put('profiles/{profile}/default', [OrganizationProfilesController::class, 'makeDefault'])->name('admin.organizations.profiles.default');
        Route::get('webhooks', [OrganizationWebhooksController::class, 'index'])->name('admin.organizations.webhooks.index');
        Route::post('webhooks', [OrganizationWebhooksController::class, 'store'])->name('admin.organizations.webhooks.store');
        Route::put('webhooks/{webhook}', [OrganizationWebhooksController::class, 'update'])->name('admin.organizations.webhooks.update');
        Route::delete('webhooks/{webhook}', [OrganizationWebhooksController::class, 'destroy'])->name('admin.organizations.webhooks.destroy');
    });
});

require __DIR__.'/settings.php';
