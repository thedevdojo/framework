<?php

namespace Tests\Feature;

use Illuminate\Contracts\Console\Kernel;
use Illuminate\Foundation\Application;
use Tests\TestCase;

/**
 * Base for the app-tier serving suites. devdojo-framework.serve and site_root are
 * read by the package's service provider at BOOT (the fallback route is
 * registered there), so a test must have them in place before the app boots.
 *
 * This overrides createApplication() to inject the two config values via an
 * $app->booting() callback — which fires after config is loaded and after the
 * package's register(), but before its boot() — then serveFrom() sets the
 * desired values and re-boots a fresh application. Every serveFrom() yields a
 * brand-new container, so one fixture's view locations never leak into the
 * next's resolution.
 */
abstract class ServingTestCase extends TestCase
{
    protected bool $serveEnabled = false;

    protected ?string $serveSiteRoot = null;

    /** @var list<string> temp site roots to clean up in tearDown */
    private array $tempRoots = [];

    public function createApplication()
    {
        $app = require Application::inferBasePath().'/bootstrap/app.php';

        $app->booting(function ($app) {
            $app['config']->set('devdojo-framework.serve', $this->serveEnabled);
            $app['config']->set('devdojo-framework.site_root', $this->serveSiteRoot);
        });

        $app->make(Kernel::class)->bootstrap();

        return $app;
    }

    protected function tearDown(): void
    {
        foreach ($this->tempRoots as $root) {
            $this->deleteTree($root);
        }
        $this->tempRoots = [];

        parent::tearDown();
    }

    /** Boot a fresh app serving $siteRoot (serve toggled by $serve). */
    protected function serveFrom(?string $siteRoot, bool $serve = true): void
    {
        $this->serveSiteRoot = $siteRoot;
        $this->serveEnabled = $serve;
        $this->refreshApplication();
    }

    /**
     * Write a throwaway site tree ($relativePath => $contents) and return its
     * root. Registered for cleanup.
     *
     * @param  array<string, string>  $files
     */
    protected function makeSite(array $files): string
    {
        $root = sys_get_temp_dir().'/pk-serving-'.uniqid();
        foreach ($files as $relative => $contents) {
            $path = $root.'/'.ltrim($relative, '/');
            @mkdir(dirname($path), 0755, true);
            file_put_contents($path, $contents);
        }
        $this->tempRoots[] = $root;

        return $root;
    }

    private function deleteTree(string $dir): void
    {
        if (! is_dir($dir)) {
            return;
        }
        $items = new \RecursiveIteratorIterator(
            new \RecursiveDirectoryIterator($dir, \FilesystemIterator::SKIP_DOTS),
            \RecursiveIteratorIterator::CHILD_FIRST
        );
        foreach ($items as $item) {
            $item->isDir() ? @rmdir($item->getPathname()) : @unlink($item->getPathname());
        }
        @rmdir($dir);
    }
}
