<?php

use App\Http\Controllers\OrganizationSwitchController;
use Illuminate\Support\Facades\Route;
use Laravel\Fortify\Features;

Route::inertia('/', 'welcome', [
    'canRegister' => Features::enabled(Features::registration()),
])->name('home');

Route::middleware(['auth', 'verified'])->group(function () {
    Route::inertia('dashboard', 'dashboard')->name('dashboard');

    Route::put('organizations/{organization}/switch', OrganizationSwitchController::class)
        ->name('organizations.switch');
});

require __DIR__.'/settings.php';
