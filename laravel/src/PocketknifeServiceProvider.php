<?php

namespace Devdojo\Pocketknife;

use Illuminate\Foundation\Vite;
use Illuminate\Support\Facades\Blade;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\View;
use Illuminate\Support\ServiceProvider;

class PocketknifeServiceProvider extends ServiceProvider
{
    public function register(): void
    {
        $this->mergeConfigFrom(__DIR__.'/../config/pocketknife.php', 'pocketknife');

        // Replace the real Vite container entry with the static projection —
        // Blade's compiled `@vite(...)` always resolves `app(Vite::class)(...)`,
        // so binding over that class name is the whole seam (see StaticVite).
        if (config('pocketknife.static_projection')) {
            $this->app->singleton(Vite::class, fn () => new StaticVite);
        }
    }

    public function boot(): void
    {
        // Let a host app publish and override the package config.
        if ($this->app->runningInConsole()) {
            $this->publishes([
                __DIR__.'/../config/pocketknife.php' => config_path('pocketknife.php'),
            ], 'pocketknife-config');
        }

        if ($root = config('pocketknife.site_root')) {
            // View path precedence — DECIDED: the host APP's views win over the
            // site's. The finder searches locations in registration order and
            // returns the first hit; the app registered its own resources/views
            // during framework boot, so appending the site root here leaves the
            // app's paths ahead of it. This is the precedence the toggle-to-app /
            // devdojo Sites integration direction requires: the host must be able
            // to shadow a site view (a layout, a shared component) without editing
            // the site tree. Append order is therefore correct as-is — do not
            // prepend. (See design spec §5.)
            View::addLocation($root.'/resources/views');
            Blade::anonymousComponentPath($root.'/resources/views/components');
            SiteData::share($this->app->make('view'), $root);

            // Opt-in serving: register a Route::fallback that turns the site
            // tree into a live app. A fallback fires ONLY when no real app route
            // matched (app routes always win — spec requirement 2), and Laravel
            // registers it for GET (+ HEAD), so other verbs are not served.
            // serve=false registers nothing: setting site_root alone stays
            // render-only (the oracle / protocol-client mode). The fallback is
            // an invokable controller (not a closure) so it survives
            // `php artisan route:cache`.
            if (config('pocketknife.serve')) {
                Route::fallback(SiteServer::class);
            }
        }
    }
}
