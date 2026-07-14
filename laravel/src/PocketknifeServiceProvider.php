<?php

namespace Devdojo\Pocketknife;

use Illuminate\Support\Facades\Blade;
use Illuminate\Support\Facades\View;
use Illuminate\Support\ServiceProvider;

class PocketknifeServiceProvider extends ServiceProvider
{
    public function register(): void
    {
        $this->mergeConfigFrom(__DIR__.'/../config/pocketknife.php', 'pocketknife');
    }

    public function boot(): void
    {
        // The one line that makes *.blade.html real Blade.
        View::addExtension('blade.html', 'blade');

        if ($root = config('pocketknife.site_root')) {
            View::addLocation($root.'/resources/views');
            Blade::anonymousComponentPath($root.'/resources/views/components');
            SiteData::share($this->app->make('view'), $root);
        }
    }
}
