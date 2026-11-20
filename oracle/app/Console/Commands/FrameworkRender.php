<?php

namespace App\Console\Commands;

use Devdojo\Framework\SiteData;
use Devdojo\Framework\SiteLayout;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Blade;
use Illuminate\Support\Facades\File;
use Illuminate\Support\Facades\View;

class FrameworkRender extends Command
{
    protected $signature = 'framework:render
        {input : Absolute path to a site root (the directory holding src/, or resources/ for an older site)}
        {entry : Dot-notation view name, e.g. pages.index}
        {bindings?* : Optional key=value data bindings (value is JSON-decoded when valid JSON, else kept as a raw string)}
        {--data= : JSON-encoded object merged over the key=value bindings, for structures they cannot express}
        {--objects : Decode --data (and JSON bindings) as stdClass objects, the shape the SiteData contract binds — needed for a dynamic page entry ($post->slug)}';

    protected $description = 'Render a single view from a site root through real Blade and print the raw rendered bytes (the manual conformance probe)';

    public function handle(): int
    {
        $input = rtrim($this->argument('input'), '/');
        $entry = $this->argument('entry');

        $data = [];
        foreach ($this->argument('bindings') as $pair) {
            if (! str_contains($pair, '=')) {
                $this->components->error("Malformed binding \"$pair\" - expected key=value.");

                return self::FAILURE;
            }
            [$key, $value] = explode('=', $pair, 2);
            $decoded = json_decode($value, ! $this->option('objects'));
            $data[$key] = json_last_error() === JSON_ERROR_NONE ? $decoded : $value;
        }
        if ($json = $this->option('data')) {
            $decoded = json_decode($json, ! $this->option('objects'));
            if (json_last_error() !== JSON_ERROR_NONE) {
                $this->components->error('--data is not valid JSON.');

                return self::FAILURE;
            }
            // The bindings map itself is always an array; with --objects only
            // the VALUES are stdClass (a dynamic page's entry, its siblings).
            $data = array_merge($data, (array) $decoded);
        }

        // A fresh `php artisan` process per invocation is the isolation
        // strategy (see FrameworkConformance): the view finder and
        // anonymous-component paths added here die with this process, so
        // one case can never leak into the next one's view resolution.
        File::ensureDirectoryExists(storage_path('framework/views'));
        config(['devdojo-framework.site_root' => $input]);
        $layout = SiteLayout::detect($input);
        View::addLocation($layout->viewsDir());
        Blade::anonymousComponentPath($layout->componentsDir());

        // site_root is set at render time, AFTER the service provider booted, so
        // the provider's boot-time binding never fired. Bind the data contract
        // here so $site / collections / content resolve for this render.
        SiteData::share($this->laravel->make('view'), $input);

        // A dynamic page ([posts.slug].blade.php) cannot be named in dot
        // notation (the finder would split on the bracketed dot), so an entry
        // given as a path relative to the folder that holds pages/ renders by file.
        $rendered = str_ends_with($entry, '.blade.php')
            ? View::file($layout->viewsDir()."/$entry", $data)->render()
            : View::make($entry, $data)->render();

        // Print the exact rendered bytes - no trailing newline, no console
        // formatting/escaping of the HTML. $this->line()/->info() would
        // both add a newline and risk Symfony Console interpreting "<...>"
        // sequences in the markup as output-formatting tags.
        fwrite(STDOUT, $rendered);

        return self::SUCCESS;
    }
}
