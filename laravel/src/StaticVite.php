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
 * Only a .js entry (which needs a real bundler) and a missing file are
 * skipped; there is no extension allow-list, so EVERY other entry — whatever
 * its extension — is inlined verbatim as a <style> block. This matches the Go
 * engine's vite.go, which likewise filters just .js and reads every remaining
 * entry (parity holds). This class has no diagnostics channel (the Go engine's
 * app-tier/error Diagnostics are engine-only — see the task report). Zero
 * resolved entries means zero output, including the CDN script — there is
 * nothing for it to serve.
 */
class StaticVite extends Vite
{
    /**
     * The Tailwind v4 Play CDN script src — kept byte-identical to the Go
     * engine's constant (blade/render/vite.go).
     */
    public const CDN = 'https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4';

    public function __invoke($entrypoints = null, $buildDirectory = null): HtmlString
    {
        // The static projection only applies when this app is actually
        // hosting a Pocketknife site (site_root configured — the oracle sets
        // it per render; the app tier sets POCKETKNIFE_SITE_ROOT). A host that
        // merely installs the package for its protocol client (the DevDojo
        // platform) must keep real Vite behavior for its own @vite calls, or
        // every page in the host app loses its build assets.
        $root = rtrim((string) config('pocketknife.site_root'), '/');

        if ($root === '') {
            return parent::__invoke($entrypoints ?? [], $buildDirectory);
        }

        // Blade usage allows a bare `@vite` / `@vite()` (compiled as a
        // zero-arg call — hence the null default the parent lacks) and a
        // single string entrypoint (`@vite('resources/css/app.css')`); real
        // Vite normalizes strings through a Collection. Mirror both here so
        // every form degrades to "no entries → no output", matching the Go
        // engine's parseViteEntries (blade/render/vite.go).
        if ($entrypoints === null) {
            $entrypoints = [];
        } elseif (is_string($entrypoints)) {
            $entrypoints = [$entrypoints];
        }

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
