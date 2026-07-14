<?php

namespace App\Console\Commands;

use Devdojo\Pocketknife\SiteData;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Blade;
use Illuminate\Support\Facades\File;
use Illuminate\Support\Facades\View;

class PocketknifeRender extends Command
{
    protected $signature = 'pocketknife:render
        {input : Absolute path to a site root (the directory holding resources/views)}
        {entry : Dot-notation view name, e.g. pages.index}
        {bindings?* : Optional key=value data bindings (value is JSON-decoded when valid JSON, else kept as a raw string)}
        {--data= : JSON-encoded object merged over the key=value bindings, for structures they cannot express}';

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
            $decoded = json_decode($value, true);
            $data[$key] = json_last_error() === JSON_ERROR_NONE ? $decoded : $value;
        }
        if ($json = $this->option('data')) {
            $decoded = json_decode($json, true);
            if (json_last_error() !== JSON_ERROR_NONE) {
                $this->components->error('--data is not valid JSON.');

                return self::FAILURE;
            }
            $data = array_merge($data, $decoded);
        }

        // A fresh `php artisan` process per invocation is the isolation
        // strategy (see PocketknifeConformance): the view finder and
        // anonymous-component paths added here die with this process, so
        // one case can never leak into the next one's view resolution.
        File::ensureDirectoryExists(storage_path('framework/views'));
        config(['pocketknife.site_root' => $input]);
        View::addLocation("$input/resources/views");
        Blade::anonymousComponentPath("$input/resources/views/components");

        // site_root is set at render time, AFTER the service provider booted, so
        // the provider's boot-time binding never fired. Bind the data contract
        // here so $site / collections / content resolve for this render.
        SiteData::share($this->laravel->make('view'), $input);

        $rendered = View::make($entry, $data)->render();

        // Print the exact rendered bytes - no trailing newline, no console
        // formatting/escaping of the HTML. $this->line()/->info() would
        // both add a newline and risk Symfony Console interpreting "<...>"
        // sequences in the markup as output-formatting tags.
        fwrite(STDOUT, $rendered);

        return self::SUCCESS;
    }
}
