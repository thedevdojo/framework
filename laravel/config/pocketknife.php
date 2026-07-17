<?php

return [
    // Absolute path to the site root (the directory holding resources/ and public/).
    'site_root' => env('POCKETKNIFE_SITE_ROOT'),

    // Serve the site tree as a live app. When true AND site_root is set, the
    // package registers a Route::fallback that serves the site's pages and
    // public/ assets — but ONLY for URLs no real app route already handles
    // (app routes always win). Default FALSE: setting site_root alone renders
    // views without ever registering a route (the render-only mode the oracle
    // and the DevDojo platform's protocol client use). See SiteServer.
    //
    // Serving details:
    //   - Routing mirrors the engine (PageResolver): "/" -> pages/index, a flat
    //     page wins over a same-named directory index, nested dirs, traversal
    //     guarded. GET/HEAD only; other verbs are not served.
    //   - {site_root}/public/{path} is served first (mime-typed, contained, no
    //     dotfiles, no directory listings) so a bare tree is self-serving.
    //   - An unresolved path renders pages/404.blade.php with a 404 status when
    //     present, else falls through to the app's own 404.
    'serve' => env('POCKETKNIFE_SERVE', false),

    // When true, @vite renders the static projection (Play CDN + inlined CSS)
    // instead of requiring a Vite build — see PocketknifeServiceProvider and
    // Devdojo\Pocketknife\StaticVite.
    'static_projection' => env('POCKETKNIFE_STATIC_PROJECTION', true),

    // --- Protocol client (Devdojo\Pocketknife\Engine\Engine) ---

    // Absolute path to the compiled `pocketknife` binary the platform shells to
    // for `pocketknife compile`. Null/empty disables the client (isAvailable()).
    'binary' => env('POCKETKNIFE_BINARY'),

    // Seconds a single compile may run before the client kills the process.
    'timeout' => (int) env('POCKETKNIFE_TIMEOUT', 30),

    // The client refuses a binary whose reported engine version is below this.
    'minimum_version' => env('POCKETKNIFE_MINIMUM_VERSION', '0.1.0'),
];
