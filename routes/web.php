<?php

use App\Http\Controllers\Admin\OrganizationProfilesController;
use App\Http\Controllers\Admin\OrganizationSettingsController;
use App\Http\Controllers\Admin\OrganizationUsersController;
use App\Http\Controllers\ManageController;
use App\Http\Controllers\OrganizationSwitchController;
use App\Http\Controllers\StatusController;
use App\Http\Middleware\EnsureOrganizationAdmin;
use App\Http\Middleware\EnsureOrganizationMember;
use Illuminate\Support\Facades\Route;

Route::redirect('/', '/manage');

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
        Route::get('profiles', [OrganizationProfilesController::class, 'index'])->name('admin.organizations.profiles.index');
        Route::get('profiles/create', [OrganizationProfilesController::class, 'create'])->name('admin.organizations.profiles.create');
        Route::post('profiles', [OrganizationProfilesController::class, 'store'])->name('admin.organizations.profiles.store');
        Route::get('profiles/{profile}/edit', [OrganizationProfilesController::class, 'edit'])->name('admin.organizations.profiles.edit');
        Route::put('profiles/{profile}', [OrganizationProfilesController::class, 'update'])->name('admin.organizations.profiles.update');
        Route::put('profiles/{profile}/default', [OrganizationProfilesController::class, 'makeDefault'])->name('admin.organizations.profiles.default');
    });
});

require __DIR__.'/settings.php';
