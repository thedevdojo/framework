<?php

namespace Devdojo\Framework\Tests;

use Devdojo\Framework\PageResolver;
use Devdojo\Framework\SiteLayout;
use PHPUnit\Framework\TestCase;

/**
 * Where a site keeps its files, answered the way the Go engine answers it
 * (site.Detect in site/site.go). The rows mirror site/site_test.go: a change to
 * one is a change to the other.
 */
class SiteLayoutTest extends TestCase
{
    private string $root;

    protected function setUp(): void
    {
        $this->root = sys_get_temp_dir().'/devdojo-site-layout-'.uniqid();
        mkdir($this->root, 0777, true);
        $this->root = realpath($this->root); // macOS keeps its temp folder behind a link
    }

    protected function tearDown(): void
    {
        $this->deleteTree($this->root);
    }

    /**
     * Laravel keys a component folder by the text of its path, and writes that
     * key into every compiled view. One site reached by two spellings of the
     * same path would otherwise leave a cached page asking for a component
     * folder that is not registered, and Laravel prints the name in its place.
     */
    public function test_one_site_has_one_spelling_of_its_folders_however_its_root_is_written(): void
    {
        $this->write('src/pages/index.blade.php', 'src/views/components/card.blade.php');

        $plain = SiteLayout::detect($this->root);
        $roundabout = SiteLayout::detect($this->root.'/src/../');

        $this->assertSame($plain->componentsDir(), $roundabout->componentsDir());
        $this->assertSame($plain->viewsDir(), $roundabout->viewsDir());
        $this->assertSame($this->root.'/src/views/components', $roundabout->componentsDir());
    }

    public function test_a_flat_src_site_is_the_default_shape(): void
    {
        $this->write('src/pages/index.blade.php', 'src/components/card.blade.php', 'src/data/site.json');

        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['src', 'src/pages', 'src/components'], [$layout->root, $layout->pages, $layout->components]);
        $this->assertSame($this->root.'/src', $layout->viewsDir());
        $this->assertSame($this->root.'/src/components', $layout->componentsDir());
        $this->assertSame($this->root.'/src/data', $layout->dataDir());
    }

    public function test_an_empty_site_looks_in_src(): void
    {
        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['src', 'src/pages', 'src/components'], [$layout->root, $layout->pages, $layout->components]);
    }

    public function test_the_old_resources_shape_still_works(): void
    {
        $this->write('resources/views/pages/index.blade.php', 'resources/views/components/card.blade.php', 'resources/data/site.json');

        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['resources', 'resources/views/pages', 'resources/views/components'], [$layout->root, $layout->pages, $layout->components]);
        $this->assertSame($this->root.'/resources/views', $layout->viewsDir());
        $this->assertSame($this->root.'/resources/data', $layout->dataDir());
    }

    public function test_views_inside_src_is_a_backup(): void
    {
        $this->write('src/views/pages/index.blade.php', 'src/views/components/card.blade.php');

        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['src/views/pages', 'src/views/components'], [$layout->pages, $layout->components]);
    }

    public function test_pages_and_components_are_found_separately(): void
    {
        $this->write('src/pages/index.blade.php', 'src/views/components/card.blade.php');

        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['src/pages', 'src/views/components'], [$layout->pages, $layout->components]);
        $this->assertSame($this->root.'/src', $layout->viewsDir());
    }

    public function test_src_wins_over_resources_and_the_flat_folder_over_views(): void
    {
        $this->write('src/pages/index.blade.php', 'src/views/pages/old.blade.php', 'resources/views/pages/older.blade.php');

        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['src', 'src/pages'], [$layout->root, $layout->pages]);
    }

    public function test_a_folder_with_no_files_yet_follows_the_other_one(): void
    {
        $this->write('resources/views/pages/index.blade.php');
        $this->assertSame('resources/views/components', SiteLayout::detect($this->root)->components);
    }

    public function test_a_resources_site_with_no_views_yet_keeps_its_old_shape(): void
    {
        $this->write('resources/data/site.json');

        $layout = SiteLayout::detect($this->root);

        $this->assertSame(['resources/views/pages', 'resources/views/components'], [$layout->pages, $layout->components]);
    }

    public function test_an_asset_named_in_the_other_folder_is_found_in_this_one(): void
    {
        $this->write('src/pages/index.blade.php');
        $layout = SiteLayout::detect($this->root);

        $this->assertSame('src/css/site.css', $layout->asset('resources/css/site.css'));
        $this->assertSame('src/css/site.css', $layout->asset('src/css/site.css'));
        $this->assertSame('public/css/app.css', $layout->asset('public/css/app.css'));
    }

    public function test_a_url_resolves_in_whichever_folder_holds_the_pages(): void
    {
        $this->write('src/pages/index.blade.php', 'src/pages/blog/index.blade.php', 'src/pages/404.blade.php');

        $this->assertSame('pages/index', PageResolver::resolve($this->root, '/'));
        $this->assertSame('pages/blog/index', PageResolver::resolve($this->root, '/blog'));
        $this->assertNull(PageResolver::resolve($this->root, '/missing'));
        $this->assertSame('pages/404', PageResolver::notFoundView($this->root));
    }

    private function write(string ...$paths): void
    {
        foreach ($paths as $rel) {
            $path = $this->root.'/'.$rel;
            if (! is_dir(dirname($path))) {
                mkdir(dirname($path), 0777, true);
            }
            file_put_contents($path, 'x');
        }
    }

    private function deleteTree(string $dir): void
    {
        if (! is_dir($dir)) {
            return;
        }
        foreach (scandir($dir) as $item) {
            if ($item === '.' || $item === '..') {
                continue;
            }
            $path = $dir.'/'.$item;
            is_dir($path) ? $this->deleteTree($path) : unlink($path);
        }
        rmdir($dir);
    }
}
