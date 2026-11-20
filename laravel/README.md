# devdojo/framework (Laravel package)

Run DevDojo Framework sites written in Blade from a real Laravel application. The
package has two independent roles:

1. **Protocol client** — `Devdojo\Framework\Engine\Engine`, a thin, typed
   wrapper that shells out to the compiled `devdojo` Go binary
   (`devdojo compile`, JSON over stdin/stdout). This is how a host whose
   site files live in a database talks to the static-tier engine.
2. **Site rendering & serving** — point `devdojo-framework.site_root` at a site tree
   and the service provider wires its views, components, and data contract
   into real Blade. Opt in with `devdojo-framework.serve` and a `Route::fallback`
   serves the whole tree as a live app (pages + `public/` assets).

Either role works without the other: the client needs only `binary`; rendering
needs only `site_root`.

## Install

The package is consumed via a Composer path repository (it is not on
Packagist). In the host app's `composer.json`:

```json
{
    "repositories": [
        { "type": "path", "url": "../framework/laravel", "options": { "symlink": true } }
    ]
}
```

```sh
composer require devdojo/framework:@dev
```

`Devdojo\Framework\FrameworkServiceProvider` is auto-discovered.

## Configuration (`config/devdojo-framework.php`)

Publish with `php artisan vendor:publish --tag=devdojo-framework-config`.

| Key | Env | Default | Meaning |
|---|---|---|---|
| `site_root` | `DEVDOJO_SITE_ROOT` | `null` | Absolute path to a site tree (the directory holding `resources/` and `public/`). Setting it registers the site's views + data bindings — render-only, no routes. |
| `serve` | `DEVDOJO_SERVE` | `false` | With `site_root`, also register the `Route::fallback` that serves the tree as a live app. |
| `static_projection` | `DEVDOJO_STATIC_PROJECTION` | `true` | Render `@vite(...)` as the static projection (Tailwind v4 Play CDN `<script>` + each CSS entry inlined in a `<style type="text/tailwindcss">` block) instead of requiring a Vite build. Byte-identical to the Go engine's `@vite` emission. |
| `binary` | `DEVDOJO_BINARY` | `null` | Absolute path to the compiled `devdojo` binary. Null/empty disables the client (`Engine::isAvailable()` is false). |
| `timeout` | `DEVDOJO_TIMEOUT` | `30` | Seconds one compile may run before the client kills the process. |
| `minimum_version` | `DEVDOJO_MINIMUM_VERSION` | `0.1.0` | The client refuses a binary whose reported engine version is lower. |

## The protocol client

```php
use Devdojo\Framework\Engine\{Engine, CompileRequest};

$engine = Engine::fromConfig();          // reads devdojo-framework.binary/timeout/minimum_version

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

On the wire this is one `devdojo compile` process per call: the request
JSON `{"files": {...}, "data": {...}?, "entries": [...]?, "options": {...}?}`
on stdin, the response
`{"engine": "x.y.z", "files": {...}, "deps": {...}, "diagnostics": [...]}`
on stdout. `options.max_loop_iterations` caps the total `@foreach` iterations
one entry may render before it aborts with an `error`/`render-budget`
diagnostic (omitted → the engine's generous default; `CompileRequest` does not
expose it — send it only from a host that runs untrusted templates).
`options.url` is accepted but **unused in v1**: it round-trips without
affecting output, reserved for a future canonical-URL rendering mode (hosts
that already send it, mirroring sitebuilder, need no change). Template problems (undefined variables, missing components,
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
- the data contract (`SiteData`), **all of it `View::share`d globally** so
  pages, layouts, and components resolve the same variables: `$site` from
  `resources/data/site.json`, one variable per collection — a
  `resources/data/collections/<name>.json` array, or a
  `resources/data/collections/<name>/<slug>.json` folder of one object per
  entry, ordered by `date` descending then file name (the folder wins when a
  name has both) — and one per `resources/data/content/<dir>/` of markdown
  files. Collections and content
  share first and `site` **last**, so a collection named `site` can never
  displace the site object. Inside a component, an explicitly **passed**
  attribute overrides a same-named binding (it is per-view data, which
  `gatherData` merges over shared data), while a declared `@props` **default**
  only fills in where the name is still null — a default cannot shadow a
  collection. Data a page was rendered with (the protocol's `data` map) still
  wins in that page, so a dynamic page's matched entry shadows its collection
  there while components keep seeing the full collection.

  *(Global scope since 2026-07-28 — see
  `docs/specs/2026-07-28-collections-global-scope-design.md`. Collections and
  content were previously bound to `pages.*` views only, which left a
  layout-instantiated nav unable to read them.)*

Pages are plain `.blade.php` views rendered by real Blade — this is the mode
the conformance oracle and the DevDojo platform use.

## Serving (`devdojo-framework.serve`)

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

The bootstrap builds a fresh `devdojo` binary from the Go source in this
repo for the end-to-end `Engine` tests; without a Go toolchain those tests
skip themselves and the pure unit tests still run.
