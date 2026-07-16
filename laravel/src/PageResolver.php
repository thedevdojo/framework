<?php

namespace Devdojo\Pocketknife;

/**
 * PageResolver maps a request URL path to the page view that serves it, using
 * the same convention as Folio and the Go engine (spec §2, and row-for-row the
 * table pinned by protocol.ResolveURL in protocol/routing.go):
 *
 *   /            -> pages/index
 *   /about       -> pages/about        (a flat file wins)
 *                -> else pages/about/index
 *   /blog        -> pages/blog/index   (directory index)
 *   (traversal)  -> null
 *   (no match)   -> null               (the 404 page is a serving concern)
 *
 * It is a pure class: no container, no framework — just filesystem lookups
 * under $siteRoot/resources/views/pages. resolve() returns the page's
 * VIEW-RELATIVE path without the ".blade.html" extension (e.g. "pages/blog/index"),
 * which the serving layer turns into a dot view name for View::make — the exact
 * mapping the conformance harness uses (str_replace('/', '.', ...)), so serving
 * and the oracle agree byte-for-byte. (A page-path segment containing a literal
 * "." is unsupported for the same reason the conformance mapping can't express
 * it — the whole system keys pages by dot view name.)
 *
 * The 404 fallback is deliberately NOT folded into resolve() the way Go's
 * ResolveURL folds it: over HTTP the 404 page carries a 404 status, so its
 * selection lives in notFoundView() (mirroring Go's notFoundOr) and the
 * provider assigns the status.
 */
class PageResolver
{
    /** View-name prefix for page views (relative to resources/views). */
    private const PAGES = 'pages';

    /** Filesystem directory (under a site root) that holds page views. */
    private const PAGES_DIR = 'resources/views/pages';

    /**
     * Resolve $urlPath to a page's view-relative path (without ".blade.html"),
     * or null when no page serves it.
     */
    public static function resolve(string $siteRoot, string $urlPath): ?string
    {
        $seg = trim($urlPath, '/');
        if ($seg === '') {
            $seg = 'index';
        }

        // Traversal guard. Go's routing.go blocks a segment that escapes the
        // pages root via path.Clean, and because its vfs is a flat literal map
        // an internal "."/".." never resolves a page there either. Refusing any
        // "."/".." segment reproduces that on a REAL filesystem (where
        // "a/../b" would otherwise normalize) and keeps serving contained.
        foreach (explode('/', $seg) as $part) {
            if ($part === '.' || $part === '..') {
                return null;
            }
        }

        $base = rtrim($siteRoot, '/').'/'.self::PAGES_DIR;

        // A flat file wins over a same-named directory index.
        if (self::isPage($base.'/'.$seg.'.blade.html')) {
            return self::PAGES.'/'.$seg;
        }
        if (self::isPage($base.'/'.$seg.'/index.blade.html')) {
            return self::PAGES.'/'.$seg.'/index';
        }

        return null;
    }

    /**
     * The site's 404 page view-relative path ("pages/404") when it exists, else
     * null — the serving-layer half of Go's notFoundOr (protocol/routing.go).
     */
    public static function notFoundView(string $siteRoot): ?string
    {
        if (self::isPage(rtrim($siteRoot, '/').'/'.self::PAGES_DIR.'/404.blade.html')) {
            return self::PAGES.'/404';
        }

        return null;
    }

    private static function isPage(string $path): bool
    {
        return is_file($path);
    }
}
