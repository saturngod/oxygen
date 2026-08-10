<?php

use Illuminate\Foundation\Inspiring;
use Illuminate\Support\Facades\Artisan;
use Illuminate\Support\Facades\Schedule;

Artisan::command('inspire', function () {
    $this->comment(Inspiring::quote());
})->purpose('Display an inspiring quote');

Schedule::command('rollups:compact-hourly')
    ->hourlyAt(5)
    ->withoutOverlapping(60)
    ->onOneServer();

Schedule::command('rollups:prune')
    ->dailyAt('01:15')
    ->withoutOverlapping(60)
    ->onOneServer();
