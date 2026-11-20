<?php

namespace Devdojo\Framework;

/**
 * SiteLayout answers where a site keeps its files, exactly as the Go engine
 * does (site.Detect in site/site.go). The two are held together by tests that
 * walk the same table (tests/SiteLayoutTest.php and site/site_test.go): a
 * change to one is a change to the other.
 *
 * A site's source folder is src/, laid out flat:
 *
 *   src/pages/         one file per page
 *   src/components/    components, with layouts/ and sections/ inside
 *   src/data/          site.json, collections/, content/
 *   src/css/           stylesheets
 *
 * Two older shapes are still read: the source folder may be resources/, and
 * pages and components may each sit one level down, under views/. A site has
 * ONE source folder, ONE pages folder and ONE components folder: where two
 * candidates both hold files the newer one is used and the other is not read.
 *
 * Unlike the engine, this class says nothing about the older shapes. A real
 * Laravel app is right to keep resources/: it is Laravel's own name for it.
 *
 * Nothing else in the package spells one of these folder names.
 */
final class SiteLayout
{
    public const SRC = 'src';

    public const RESOURCES = 'resources';

    private function __construct(
        /** Absolute path of the site (the directory that holds src/ and public/). */
        public readonly string $siteRoot,
        /** The source folder, relative: "src" or "resources". */
        public readonly string $root,
        /** The pages folder, relative: "src/pages". */
        public readonly string $pages,
        /** The components folder, relative: "src/components". */
        public readonly string $components,
    ) {}

    public static function detect(string $siteRoot): self
    {
        // One spelling per site. Laravel keys a component folder by the text of
        // its path and writes that key into every compiled view, so the same
        // site reached as `/a/site` and `/a/x/../site` would leave a cached page
        // asking for a folder nobody registered. A root that does not exist
        // yet is kept as written.
        $siteRoot = rtrim(realpath($siteRoot) ?: $siteRoot, '/');
        $has = static fn (string $rel): bool => self::hasFiles($siteRoot.'/'.$rel);

        $root = self::SRC;
        if (! $has(self::SRC) && $has(self::RESOURCES)) {
            $root = self::RESOURCES;
        }

        // Each of the two view folders is found on its own: flat first, then
        // under views/. One with no files yet follows the other; with neither,
        // src/ is flat and resources/ keeps the shape it always had.
        $nested = [];
        $picked = [];
        foreach (['pages', 'components'] as $name) {
            if ($has("$root/$name")) {
                $picked[$name] = "$root/$name";
                $nested[$name] = false;
            } elseif ($has("$root/views/$name")) {
                $picked[$name] = "$root/views/$name";
                $nested[$name] = true;
            }
        }
        foreach (['pages' => 'components', 'components' => 'pages'] as $name => $other) {
            if (isset($picked[$name])) {
                continue;
            }
            $under = $nested[$other] ?? $root === self::RESOURCES;
            $picked[$name] = $under ? "$root/views/$name" : "$root/$name";
        }

        return new self($siteRoot, $root, $picked['pages'], $picked['components']);
    }

    /** Absolute path of the pages folder. */
    public function pagesDir(): string
    {
        return $this->siteRoot.'/'.$this->pages;
    }

    /** Absolute path of the components folder. */
    public function componentsDir(): string
    {
        return $this->siteRoot.'/'.$this->components;
    }

    /**
     * The folder that holds pages/: the view location a page's name
     * ("pages.blog.index") is found under.
     */
    public function viewsDir(): string
    {
        return dirname($this->pagesDir());
    }

    /** Absolute path of the data folder (site.json, collections/, content/). */
    public function dataDir(): string
    {
        return $this->siteRoot.'/'.$this->root.'/data';
    }

    /**
     * Map a path a template names (a @vite entry) onto this site's source
     * folder: a site moved from resources/ to src/ may still say
     * `resources/css/site.css`, and the file is read from src/css/site.css
     * either way. Mirrors site.Layout.Asset.
     */
    public function asset(string $path): string
    {
        $other = ($this->root === self::RESOURCES ? self::SRC : self::RESOURCES).'/';
        if (str_starts_with($path, $other)) {
            return $this->root.'/'.substr($path, strlen($other));
        }

        return $path;
    }

    /** Whether $dir holds at least one file, at any depth. */
    private static function hasFiles(string $dir): bool
    {
        if (! is_dir($dir)) {
            return false;
        }
        $items = new \RecursiveIteratorIterator(
            new \RecursiveDirectoryIterator($dir, \FilesystemIterator::SKIP_DOTS)
        );
        foreach ($items as $item) {
            if ($item->isFile()) {
                return true;
            }
        }

        return false;
    }
}
