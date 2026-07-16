<?php

namespace Devdojo\Pocketknife\Tests;

use Devdojo\Pocketknife\PageResolver;
use PHPUnit\Framework\TestCase;

/**
 * Pins the URL -> page routing table in the PHP tier row-for-row against the Go
 * engine's contract. The rows below are copied verbatim from
 * protocol/routing_test.go (TestResolveURL / TestResolveURLNo404 /
 * TestResolveURLTraversalGuarded), which drives protocol.ResolveURL
 * (protocol/routing.go). The tree here mirrors that test's routingTree().
 *
 * One intentional split from the Go side: Go's ResolveURL folds the 404
 * fallback into its single return value ("/missing" -> pages/404 when present).
 * In the PHP serving tier the 404 page is a status-bearing serving concern, so
 * PageResolver::resolve() returns null for an unmatched/traversal path and the
 * 404 page is selected by PageResolver::notFoundView() (mirroring Go's
 * notFoundOr). Both halves are asserted below, so the full Go table is covered.
 */
class PageResolverTest extends TestCase
{
    private string $root;

    protected function setUp(): void
    {
        // Mirror protocol/routing_test.go routingTree().
        $this->root = sys_get_temp_dir().'/pk-page-resolver-'.uniqid();
        $this->writePage('index.blade.html', 'home');
        $this->writePage('about.blade.html', 'about-flat');
        $this->writePage('about/index.blade.html', 'about-nested');
        $this->writePage('blog/index.blade.html', 'blog');
        $this->writePage('404.blade.html', 'not found');
    }

    protected function tearDown(): void
    {
        $this->deleteTree($this->root);
    }

    /**
     * TestResolveURL (protocol/routing_test.go): root -> index, a flat file wins
     * over a same-named directory index, a directory index serves its path.
     */
    public function test_resolve_url_table(): void
    {
        $cases = [
            ['/', 'pages/index'],
            ['', 'pages/index'],
            ['/about', 'pages/about'],       // flat wins over about/index
            ['/about/', 'pages/about'],      // trailing slash ignored
            ['/blog', 'pages/blog/index'],   // directory index
        ];

        foreach ($cases as [$url, $entry]) {
            $this->assertSame($entry, PageResolver::resolve($this->root, $url), "resolve($url)");
        }
    }

    /**
     * The Go "/missing" -> 404 row, split: resolve() misses (null) and
     * notFoundView() selects the 404 page.
     */
    public function test_unmatched_path_is_null_and_404_page_is_selected(): void
    {
        $this->assertNull(PageResolver::resolve($this->root, '/missing'));
        $this->assertSame('pages/404', PageResolver::notFoundView($this->root));
    }

    /**
     * TestResolveURLNo404 (protocol/routing_test.go): with no 404 page an
     * unmatched URL resolves to nothing, and notFoundView() is null.
     */
    public function test_no_404_page(): void
    {
        $bare = sys_get_temp_dir().'/pk-page-resolver-bare-'.uniqid();
        mkdir($bare.'/resources/views/pages', 0755, true);
        file_put_contents($bare.'/resources/views/pages/index.blade.html', 'home');

        try {
            $this->assertNull(PageResolver::resolve($bare, '/missing'));
            $this->assertNull(PageResolver::notFoundView($bare));
        } finally {
            $this->deleteTree($bare);
        }
    }

    /**
     * TestResolveURLTraversalGuarded (protocol/routing_test.go): a URL that would
     * escape the pages root resolves to nothing, never a file outside pages/.
     */
    public function test_traversal_guarded(): void
    {
        $this->assertNull(PageResolver::resolve($this->root, '/../secret'));
        $this->assertNull(PageResolver::resolve($this->root, '/about/../../secret'));
        $this->assertNull(PageResolver::resolve($this->root, '/./index'));
    }

    private function writePage(string $relative, string $body): void
    {
        $path = $this->root.'/resources/views/pages/'.$relative;
        @mkdir(dirname($path), 0755, true);
        file_put_contents($path, $body);
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
