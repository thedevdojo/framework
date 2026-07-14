<?php

namespace Devdojo\Pocketknife;

use Illuminate\Foundation\Vite;
use Illuminate\Support\HtmlString;

/**
 * StaticVite replaces Illuminate\Foundation\Vite's __invoke() with the static
 * projection: no Vite build, no manifest, no dev server. It is bound over the
 * real Vite class (see PocketknifeServiceProvider) so Blade's compiled
 * `@vite(...)` — which resolves `app(Vite::class)(...)` regardless — renders
 * plain, self-contained HTML:
 *
 *   <script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>
 *   <style type="text/tailwindcss">CONTENTS_OF_entry.css</style>
 *
 * one CDN <script> (even with multiple entries), then one <style> per CSS
 * entry in argument order, each entry's file content inlined byte-verbatim
 * (no escaping, no trimming) and joined by a single "\n". This is the exact
 * emission blade/render/vite.go produces for ast.Vite on the Go engine — see
 * that file for the parity contract; a shared oracle fixture
 * (fixtures/vite-css) keeps the two byte-identical.
 *
 * Non-CSS entries (.js, or anything else) and missing CSS files are silently
 * skipped: real bundling needs a real bundler, and this class has no
 * diagnostics channel (the Go engine's app-tier/error Diagnostics are
 * engine-only — see the task report). Zero resolved CSS entries means zero
 * output, including the CDN script — there is nothing for it to serve.
 */
class StaticVite extends Vite
{
    /**
     * The Tailwind v4 Play CDN script src — kept byte-identical to the Go
     * engine's constant (blade/render/vite.go).
     */
    public const CDN = 'https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4';

    public function __invoke($entrypoints, $buildDirectory = null): HtmlString
    {
        // Blade usage allows a bare `@vite` (no args → null) and a single
        // string entrypoint (`@vite('resources/css/app.css')`); real Vite
        // normalizes both through a Collection. Mirror that here.
        if ($entrypoints === null) {
            $entrypoints = [];
        } elseif (is_string($entrypoints)) {
            $entrypoints = [$entrypoints];
        }

        $root = rtrim((string) config('pocketknife.site_root'), '/');

        $styles = [];
        foreach ($entrypoints as $entry) {
            if (! is_string($entry) || str_ends_with($entry, '.js')) {
                continue; // app-tier: needs a real bundler, not the static projection
            }
            $path = $root.'/'.ltrim($entry, '/');
            if (! is_file($path)) {
                continue; // missing asset: skipped, no diagnostics channel here
            }
            $styles[] = (string) file_get_contents($path);
        }

        if ($styles === []) {
            return new HtmlString('');
        }

        $tags = ['<script src="'.self::CDN.'"></script>'];
        foreach ($styles as $css) {
            $tags[] = '<style type="text/tailwindcss">'.$css.'</style>';
        }

        return new HtmlString(implode("\n", $tags));
    }
}
