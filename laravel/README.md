# devdojo/pocketknife (Laravel package)

Run Pocketknife (portable Blade) sites from a real Laravel application. The
package has two independent roles:

1. **Protocol client** — `Devdojo\Pocketknife\Engine\Engine`, a thin, typed
   wrapper that shells out to the compiled `pocketknife` Go binary
   (`pocketknife compile`, JSON over stdin/stdout). This is how a host whose
   site files live in a database talks to the static-tier engine.
2. **Site rendering & serving** — point `pocketknife.site_root` at a site tree
   and the service provider wires its views, components, and data contract
   into real Blade. Opt in with `pocketknife.serve` and a `Route::fallback`
   serves the whole tree as a live app (pages + `public/` assets).

Either role works without the other: the client needs only `binary`; rendering
needs only `site_root`.

## Install

The package is consumed via a Composer path repository (it is not on
Packagist). In the host app's `composer.json`:

```json
{
    "repositories": [
        { "type": "path", "url": "../pocketknife/laravel", "options": { "symlink": true } }
    ]
}
```

```sh
composer require devdojo/pocketknife:@dev
```

`Devdojo\Pocketknife\PocketknifeServiceProvider` is auto-discovered.

## Configuration (`config/pocketknife.php`)

Publish with `php artisan vendor:publish --tag=pocketknife-config`.

| Key | Env | Default | Meaning |
|---|---|---|---|
| `site_root` | `POCKETKNIFE_SITE_ROOT` | `null` | Absolute path to a site tree (the directory holding `resources/` and `public/`). Setting it registers the site's views + data bindings — render-only, no routes. |
| `serve` | `POCKETKNIFE_SERVE` | `false` | With `site_root`, also register the `Route::fallback` that serves the tree as a live app. |
| `static_projection` | `POCKETKNIFE_STATIC_PROJECTION` | `true` | Render `@vite(...)` as the static projection (Tailwind v4 Play CDN `<script>` + each CSS entry inlined in a `<style type="text/tailwindcss">` block) instead of requiring a Vite build. Byte-identical to the Go engine's `@vite` emission. |
| `binary` | `POCKETKNIFE_BINARY` | `null` | Absolute path to the compiled `pocketknife` binary. Null/empty disables the client (`Engine::isAvailable()` is false). |
| `timeout` | `POCKETKNIFE_TIMEOUT` | `30` | Seconds one compile may run before the client kills the process. |
| `minimum_version` | `POCKETKNIFE_MINIMUM_VERSION` | `0.1.0` | The client refuses a binary whose reported engine version is lower. |

## The protocol client

```php
use Devdojo\Pocketknife\Engine\{Engine, CompileRequest};

$engine = Engine::fromConfig();          // reads pocketknife.binary/timeout/minimum_version

if ($engine->isAvailable()) {
    $result = $engine->compile(new CompileRequest(
        files:   ['resources/views/pages/index.blade.php' => '<h1>{{ $title }}</h1>'],
        data:    ['title' => 'Hello'],   // optional, merged over the site's own data
        entries: [],                     // optional; empty = render every page
    ));

    $result->file('index.html');         // rendered output, keyed by OUTPUT path
    $result->deps;                       // output path => every source read OR looked for
                                         // (missing components / @vite assets included, so a
                                         // render-on-save host re-renders once they exist)
    $result->errors();                   // Diagnostic[] with kind "error"
    $result->appTier();                  // Diagnostic[] with kind "app-tier"
}
```

On the wire this is one `pocketknife compile` process per call: the request
JSON `{"files": {...}, "data": {...}?, "entries": [...]?}` on stdin, the
response `{"engine": "x.y.z", "files": {...}, "deps": {...}, "diagnostics": [...]}`
on stdout. Template problems (undefined variables, missing components,
app-tier constructs) are **data** — they arrive as `Diagnostic` objects
(`file`, `line`, `col`, `kind`, `construct`, `message`) alongside rendered
output. Only process-level failures throw: `BinaryNotFoundException`,
`BinaryFailedException` (non-zero exit or timeout), `InvalidResponseException`
(malformed JSON), `EngineVersionException` (version below the minimum).

## Rendering a site tree

With `site_root` set, the provider registers at boot:

- the site's `resources/views` as a view location — **appended**, so the host
  app's own views win on collision (the host can shadow a site view without
  editing the tree);
- `resources/views/components` as an anonymous-component path;
- the data contract (`SiteData`): `$site` from `resources/data/site.json`
  (shared globally, components see it), one variable per
  `resources/data/collections/<name>.json` and per
  `resources/data/content/<dir>/` of markdown files — bound to `pages.*`
  views only, never overriding data a page was rendered with.

Pages are plain `.blade.php` views rendered by real Blade — this is the mode
the conformance oracle and the DevDojo platform use.

## Serving (`pocketknife.serve`)

When `serve` and `site_root` are both set, the provider registers
`Route::fallback(SiteServer::class)` — an invokable controller (survives
`route:cache`) that fires **only when no real app route matched**, so app
routes always win. GET/HEAD only. Resolution order per request:

1. `{site_root}/public/{path}` — a real, contained, non-dotfile asset is
   served mime-typed (`AssetMime`); no directory listings, no traversal.
2. A page via `PageResolver` — rendered as real Blade, status 200.
3. `pages/404.blade.php` when present — rendered, status 404.
4. `abort(404)` — the app's own 404 handling.

`PageResolver` maps URL paths to page views with the same conventions as the
Go engine's `protocol.ResolveURL` (and Folio):

| URL | View |
|---|---|
| `/` | `pages/index` |
| `/about` | `pages/about`, else `pages/about/index` (a flat file wins) |
| `/blog` | `pages/blog/index` |
| traversal (`.`/`..` segments) | refused |

## Tests

```sh
composer test
```

The bootstrap builds a fresh `pocketknife` binary from the Go source in this
repo for the end-to-end `Engine` tests; without a Go toolchain those tests
skip themselves and the pure unit tests still run.
