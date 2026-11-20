<?php

namespace App\Console\Commands;

use Illuminate\Console\Command;
use Illuminate\Support\Facades\File;
use Illuminate\Support\Facades\Process;

class FrameworkConformance extends Command
{
    protected $signature = 'framework:conformance {fixtures} {--case= : run a single case}';

    protected $description = 'Render fixture entries through real Blade (one fresh process per entry) and byte-diff against expected/';

    public function handle(): int
    {
        $failures = 0;

        foreach (File::directories($this->argument('fixtures')) as $dir) {
            $name = basename($dir);
            if ($this->option('case') && $this->option('case') !== $name) {
                continue;
            }

            $case = json_decode(File::get("$dir/case.json"), true);
            if (($case['oracle'] ?? true) === false) {
                continue;
            }

            foreach ($case['entries'] as $entry) {
                // entry: resources/views/pages/index.blade.php -> view name pages.index, output index.html
                $view = str_replace('/', '.', preg_replace('#^resources/views/(.+)\.blade\.php$#', '$1', $entry));
                $out = preg_replace('#^pages\.#', '', $view);
                $outFile = str_replace('.', '/', $out).'.html';

                // Real per-case isolation: `framework:render` runs as a
                // fresh `php artisan` process for every entry rather than
                // rendering in this long-lived process. Laravel's view
                // finder and anonymous-component paths are additive with no
                // public reset API (the brief's `View::flushFinderViews()`
                // isn't a real Laravel 11 method), so a shared container
                // would let one case's view paths leak into the next case's
                // resolution - especially dangerous for same-named views
                // across cases. A new process per entry makes that
                // impossible: the paths this adds die with the process.
                $result = Process::path(base_path())->run([
                    PHP_BINARY,
                    'artisan',
                    'framework:render',
                    "$dir/input",
                    $view,
                    '--data='.json_encode($case['data'] ?? []),
                ]);

                if (! $result->successful()) {
                    $failures++;
                    $this->error("FAIL $name :: $outFile (render process exited {$result->exitCode()})");
                    $this->line(trim($result->errorOutput()));

                    continue;
                }

                $rendered = $result->output();
                $expected = File::get("$dir/expected/$outFile");

                if ($rendered !== $expected) {
                    $failures++;
                    $this->error("FAIL $name :: $outFile");
                    $this->line($this->firstDiff($expected, $rendered));
                } else {
                    $this->info("ok   $name :: $outFile");
                }
            }
        }

        return $failures === 0 ? self::SUCCESS : self::FAILURE;
    }

    private function firstDiff(string $want, string $got): string
    {
        $len = min(strlen($want), strlen($got));
        for ($i = 0; $i < $len; $i++) {
            if ($want[$i] !== $got[$i]) {
                return sprintf('first diff at byte %d: want %s got %s',
                    $i, json_encode(substr($want, $i, 40)), json_encode(substr($got, $i, 40)));
            }
        }

        return sprintf('length differs: want %d got %d', strlen($want), strlen($got));
    }
}
