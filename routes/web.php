<?php

use App\Http\Controllers\Admin\OrganizationSettingsController;
use App\Http\Controllers\Admin\OrganizationUsersController;
use App\Http\Controllers\OrganizationSwitchController;
use App\Http\Middleware\EnsureOrganizationAdmin;
use Illuminate\Support\Facades\Route;
use Laravel\Fortify\Features;

Route::inertia('/', 'welcome', [
    'canRegister' => Features::enabled(Features::registration()),
])->name('home');

Route::middleware(['auth', 'verified'])->group(function () {
    Route::inertia('dashboard', 'dashboard')->name('dashboard');
    Route::inertia('manage', 'manage')->name('manage');
    Route::inertia('status', 'status')->name('status');

    Route::put('organizations/{organization}/switch', OrganizationSwitchController::class)
        ->name('organizations.switch');

    Route::prefix('admin/organizations/{organization}')->middleware(EnsureOrganizationAdmin::class)->group(function () {
        Route::get('settings', [OrganizationSettingsController::class, 'edit'])->name('admin.organizations.settings.edit');
        Route::post('settings', [OrganizationSettingsController::class, 'update'])->name('admin.organizations.settings.update');
        Route::get('users', [OrganizationUsersController::class, 'index'])->name('admin.organizations.users.index');
    });
});

require __DIR__.'/settings.php';
