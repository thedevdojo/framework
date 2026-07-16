<?php

namespace Devdojo\Pocketknife;

use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Illuminate\Support\Facades\View;

/**
 * SiteServer turns a resolved request into a served response for a Pocketknife
 * site rooted at $siteRoot. It is the body of the Route::fallback the service
 * provider registers when pocketknife.serve is on (spec: "App-Tier Serving"),
 * so it runs ONLY after every real app route has already failed to match — app
 * routes always win.
 *
 * The order (spec §Behavior):
 *   (a) a file under {site_root}/public/  -> serve it with the right mime type
 *       (realpath-contained, no dotfiles, no directory listings);
 *   (b) a page resolved by PageResolver     -> render it as real Blade, 200;
 *   (c) the site's pages/404.blade.html      -> render it, 404;
 *   (d) neither                              -> abort(404), the app's own 404.
 *
 * Rendering is real Laravel Blade with the provider's already-registered site
 * view location and $site / collections / content bindings (SiteData) — no
 * editor markers, no diagnostics; a Blade error behaves like any app error.
 *
 * It is an INVOKABLE controller (registered as Route::fallback(SiteServer::class)),
 * not a closure, so the fallback survives `php artisan route:cache`. The site
 * root is read from config at request time — the provider only registers this
 * when pocketknife.serve AND site_root are set.
 */
class SiteServer
{
    public function __invoke(Request $request): Response
    {
        $siteRoot = (string) config('pocketknife.site_root');
        if ($siteRoot === '') {
            abort(404); // serving without a root configured — nothing to serve
        }

        // GET/HEAD only. The fallback route is registered for GET (Laravel adds
        // HEAD), so other verbs never reach here; this guard documents the
        // contract and stays correct if the route is ever widened. HEAD is
        // dispatched through the GET path and Laravel strips the body.
        if (! in_array($request->getMethod(), ['GET', 'HEAD'], true)) {
            abort(404);
        }

        // Request::path() is the RAW (still percent-encoded) path with
        // surrounding slashes trimmed ("/" -> "", "/about/" -> "about"). Leaving
        // it encoded is deliberate: a literal "%2e%2e" segment misses realpath
        // and never decodes into a "..". Normalise the root to "".
        $path = trim($request->path(), '/');

        if ($asset = $this->asset($siteRoot, $path)) {
            return $asset;
        }

        if ($view = PageResolver::resolve($siteRoot, $path)) {
            return $this->render($view, 200);
        }

        if ($notFound = PageResolver::notFoundView($siteRoot)) {
            return $this->render($notFound, 404);
        }

        abort(404);
    }

    /**
     * Serve {site_root}/public/{path} when it is a real, contained, non-dotfile
     * file; otherwise null (fall through to page resolution). No directory
     * listings, no dotfiles, and realpath containment guards traversal.
     */
    private function asset(string $siteRoot, string $path): ?Response
    {
        if ($path === '') {
            return null; // "/" is the index page, never a public/ listing
        }

        // Reject a dotfile at any level (…/.env, …/.git/config) before touching
        // the filesystem.
        foreach (explode('/', $path) as $part) {
            if ($part !== '' && $part[0] === '.') {
                return null;
            }
        }

        $base = realpath(rtrim($siteRoot, '/').'/public');
        if ($base === false) {
            return null; // no public/ dir
        }

        $target = realpath($base.'/'.$path);
        if ($target === false || ! is_file($target)) {
            return null; // missing, or a directory (no listings)
        }

        // Containment: the resolved file must live inside public/.
        if ($target !== $base && ! str_starts_with($target, $base.DIRECTORY_SEPARATOR)) {
            return null;
        }

        // Defense in depth: re-check the RESOLVED path's segments for a dotfile.
        // The segment loop above guards the request path, but a symlink INSIDE
        // public/ pointing at a sibling dotfile (public/config -> public/.env)
        // would otherwise pass — realpath resolves the link. Reject any dotfile
        // component in the contained-relative resolved path.
        $relative = substr($target, strlen($base) + 1);
        foreach (explode(DIRECTORY_SEPARATOR, $relative) as $part) {
            if ($part !== '' && $part[0] === '.') {
                return null;
            }
        }

        return new Response(
            (string) file_get_contents($target),
            200,
            ['Content-Type' => AssetMime::for($target)],
        );
    }

    private function render(string $viewRelative, int $status): Response
    {
        // View-relative path -> dot view name (pages/blog/index -> pages.blog.index),
        // the exact mapping the conformance harness uses so serving and the
        // oracle agree byte-for-byte. The site view location + SiteData bindings
        // were registered by the service provider's boot().
        $name = str_replace('/', '.', $viewRelative);

        return new Response(
            View::make($name)->render(),
            $status,
            ['Content-Type' => 'text/html; charset=UTF-8'],
        );
    }
}
