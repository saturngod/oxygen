<?php

namespace App\Providers;

use App\Contracts\LiveStreamAnalyticsPurger;
use App\Contracts\LiveStreamAnalyticsReader;
use App\Services\LiveStreamViewerAnalytics;
use App\Services\RemoteLiveStreamAnalytics;
use Carbon\CarbonImmutable;
use Illuminate\Support\Facades\Date;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\ServiceProvider;
use Illuminate\Validation\Rules\Password;

class AppServiceProvider extends ServiceProvider
{
    /**
     * Register any application services.
     */
    public function register(): void
    {
        $this->app->bind(LiveStreamAnalyticsReader::class, function ($app): LiveStreamAnalyticsReader {
            if (filled(config('services.analytics.url'))) {
                return $app->make(RemoteLiveStreamAnalytics::class);
            }

            return $app->make(LiveStreamViewerAnalytics::class);
        });

        $this->app->bind(LiveStreamAnalyticsPurger::class, function ($app): LiveStreamAnalyticsPurger {
            if (filled(config('services.analytics.url'))) {
                return $app->make(RemoteLiveStreamAnalytics::class);
            }

            return $app->make(LiveStreamViewerAnalytics::class);
        });
    }

    /**
     * Bootstrap any application services.
     */
    public function boot(): void
    {
        $this->configureDefaults();
    }

    /**
     * Configure default behaviors for production-ready applications.
     */
    protected function configureDefaults(): void
    {
        Date::use(CarbonImmutable::class);

        DB::prohibitDestructiveCommands(
            app()->isProduction(),
        );

        Password::defaults(fn (): ?Password => app()->isProduction()
            ? Password::min(12)
                ->mixedCase()
                ->letters()
                ->numbers()
                ->symbols()
                ->uncompromised()
            : null,
        );
    }
}
