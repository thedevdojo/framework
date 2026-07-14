<?php

return [
    // Absolute path to the site root (the directory holding resources/ and public/).
    'site_root' => env('POCKETKNIFE_SITE_ROOT'),

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
