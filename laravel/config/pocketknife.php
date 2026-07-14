<?php

return [
    // Absolute path to the site root (the directory holding resources/ and public/).
    'site_root' => env('POCKETKNIFE_SITE_ROOT'),

    // When true, @vite renders the static projection (Play CDN + inlined CSS)
    // instead of requiring a Vite build. Task 10 implements this.
    'static_projection' => env('POCKETKNIFE_STATIC_PROJECTION', true),
];
