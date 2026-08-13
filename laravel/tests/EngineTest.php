<?php

namespace Devdojo\Pocketknife\Tests;

use Devdojo\Pocketknife\Engine\CompileRequest;
use Devdojo\Pocketknife\Engine\Engine;
use Devdojo\Pocketknife\Engine\Exceptions\EngineVersionException;
use PHPUnit\Framework\TestCase;

/**
 * Drives the real `pocketknife` binary over the JSON protocol. Every test here
 * needs the compiled binary; without Go on the box they skip with a clear
 * message rather than fail. The binary-free failure paths (missing/empty/garbage
 * binary) live in EngineExceptionPathsTest so they run everywhere.
 */
class EngineTest extends TestCase
{
    protected function setUp(): void
    {
        if (! POCKETKNIFE_BINARY_AVAILABLE) {
            $this->markTestSkipped(
                'The pocketknife binary could not be built (is Go installed?): '.POCKETKNIFE_BUILD_OUTPUT
            );
        }
    }

    private function engine(string $minimumVersion = '0.1.0'): Engine
    {
        return new Engine(POCKETKNIFE_TEST_BINARY, 30, $minimumVersion);
    }

    public function test_it_reports_the_binary_as_available(): void
    {
        $this->assertTrue($this->engine()->isAvailable());
    }

    public function test_it_compiles_a_one_page_site(): void
    {
        $result = $this->engine()->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '<x-layouts.main><h1>{{ $site->title }}</h1></x-layouts.main>',
            'resources/views/components/layouts/main.blade.php' => '<!DOCTYPE html><body>{{ $slot }}</body>',
            'resources/data/site.json' => '{"title":"Hello"}',
        ]));

        // engine + files populated.
        $this->assertSame('0.2.0', $result->engine);
        $this->assertSame(
            '<!DOCTYPE html><body><h1>Hello</h1></body>',
            $result->file('index.html')
        );

        // deps: output → the source files read to render it.
        $this->assertArrayHasKey('index.html', $result->deps);
        $this->assertContains(
            'resources/views/components/layouts/main.blade.php',
            $result->deps['index.html']
        );

        // A clean compile carries no diagnostics.
        $this->assertSame([], $result->diagnostics);
        $this->assertSame([], $result->errors());
        $this->assertSame([], $result->appTier());
    }

    public function test_app_tier_construct_is_data_and_the_page_still_renders(): void
    {
        $result = $this->engine()->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '@auth<p>secret</p>@endauth',
        ]));

        // No exception; the app-tier construct is reported as data.
        $appTier = $result->appTier();
        $this->assertNotEmpty($appTier);
        $this->assertTrue($appTier[0]->isAppTier());
        $this->assertSame('@auth', $appTier[0]->construct);

        // The page still rendered.
        $this->assertNotNull($result->file('index.html'));

        // app-tier is not an error.
        $this->assertSame([], $result->errors());
    }

    public function test_error_diagnostic_is_data_not_an_exception(): void
    {
        $result = $this->engine()->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '<p>{{ $undefined }}</p>',
        ]));

        $errors = $result->errors();
        $this->assertNotEmpty($errors);
        $this->assertTrue($errors[0]->isError());
        $this->assertSame(
            $errors,
            $result->diagnosticsFor('resources/views/pages/index.blade.php')
        );

        // The page is still returned — errors are markers/empties, not aborts.
        $this->assertNotNull($result->file('index.html'));
    }

    public function test_version_below_minimum_throws(): void
    {
        $this->expectException(EngineVersionException::class);
        $this->engine('99.0.0')->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '<p>hi</p>',
        ]));
    }

    public function test_large_payload_does_not_deadlock(): void
    {
        // A page whose source and rendered output both far exceed a pipe's
        // 64KB buffer in BOTH directions — the classic write-all-then-read
        // deadlock. The interleaved stream_select loop must clear it.
        $big = str_repeat('devdojo ', 200000); // ~1.6MB of pass-through text

        $result = $this->engine()->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '<article>'.$big.'</article>',
        ]));

        $this->assertSame('<article>'.$big.'</article>', $result->file('index.html'));
        $this->assertSame([], $result->diagnostics);
    }
}
