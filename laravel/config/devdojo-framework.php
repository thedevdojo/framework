<?php

return [
    // Absolute path to the site root (the directory holding src/ and public/).
    // A site that still keeps its source in resources/ is read from there.
    'site_root' => env('DEVDOJO_SITE_ROOT'),

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
    'serve' => env('DEVDOJO_SERVE', false),

    // When true, @vite renders the static projection (Play CDN + inlined CSS)
    // instead of requiring a Vite build — see FrameworkServiceProvider and
    // Devdojo\Framework\StaticVite.
    'static_projection' => env('DEVDOJO_STATIC_PROJECTION', true),

    // --- Protocol client (Devdojo\Framework\Engine\Engine) ---

    // Absolute path to the compiled `devdojo` binary the platform shells to
    // for `devdojo compile`. Null/empty disables the client (isAvailable()).
    'binary' => env('DEVDOJO_BINARY'),

    // Seconds a single compile may run before the client kills the process.
    'timeout' => (int) env('DEVDOJO_TIMEOUT', 30),

    // The client refuses a binary whose reported engine version is below this.
    'minimum_version' => env('DEVDOJO_MINIMUM_VERSION', '0.1.0'),
];
