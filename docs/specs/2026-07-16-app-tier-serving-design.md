# Pocketknife Phase 4a — App-Tier Serving

**Design spec · 2026-07-16 · Status: IMPLEMENTED (Phase 4a shipped 2026-07-16 — see "Implementation" below) · Repo: pocketknife (main)**

The `devdojo/pocketknife` package learns to **serve** a Blade site tree as a
live Laravel application. This is the substrate for both convergence products:
DevDojo's toggle-to-app (a site boots as an app) and the `devdojo/sites`
Laravel integration (publish pushes source into a user's app; the package
renders it natively). Boundary: **pocketknife = render + serve a tree;
devdojo/sites (a later phase) = own the tree** (sync/tokens/eject).

## Requirements (inherited from the integration plan)

1. **Opt-in**: `pocketknife.serve` config (env `POCKETKNIFE_SERVE`, default
   FALSE). Setting `site_root` alone never registers routes (the oracle uses
   site_root for render tests without serving).
2. **App routes always win**: pages register via Laravel's fallback mechanism
   (fires only when no app route matches). `/login`, `/dashboard` are never
   shadowed. App views also win (the already-decided `addLocation` precedence).
3. **Configurable root**: the same primitive serves `resources/sites/` inside
   a user's app or a whole site tree in a toggle-to-app boot.

## Behavior

- **Routing table = the engine's, exactly** (the contract pinned by
  `protocol.ResolveURL` + the `routing-table` fixture): `/` → `pages/index`,
  `/about` → `pages/about.blade.php` else `pages/about/index.blade.php`
  (flat wins), nested paths, traversal-guarded. A PHP `PageResolver` in the
  package mirrors the table; its tests assert the same rows the Go side pins.
- **Rendering**: the resolved page renders as a real Blade view with the
  package's existing bindings ($site, collections, content, @vite static
  projection when no build exists). No editor markers, no diagnostics — this
  is real Laravel; real Blade errors behave like any app error.
- **404**: unresolved path + site has `pages/404.blade.php` → render it with
  status 404; else fall through to the app's own 404.
- **Methods**: GET/HEAD only; other verbs fall through untouched.
- **Site public assets**: when the request path matches a file under
  `{site_root}/public/`, serve it with the right mime type (traversal-guarded,
  realpath-contained). This makes a bare tree fully self-serving for the
  toggle-to-app boot; in the devdojo/sites integration the sync will typically
  place assets in the app's real public dir instead, and app-public wins
  (fallback only fires when nothing else — including the web server's static
  handling — matched).

## The proof (the phase's bar)

Byte-parity over HTTP: an oracle-side (Laravel 12) test suite that, for EVERY
oracle-checked conformance fixture, points `site_root` at the fixture input,
enables serving, requests each page's URL through Laravel's HTTP layer, and
asserts the response body is byte-identical to the fixture's `expected/`
output (which the Go engine also matches — three-way agreement). Plus: 404
semantics, app-route-precedence (an app route at a page's URL wins),
GET-only, and asset serving each pinned by tests.

## Out of scope

Everything `devdojo/sites` owns (sync/publish/tokens/webhooks/eject/manifest);
response caching; route prefixes/mounting-under-a-path; the DevDojo platform
toggle UI; php-wasm packaging.

## Implementation (Phase 4a, 2026-07-16)

Shipped in the `devdojo/pocketknife` Laravel package (`laravel/`):

- **`PageResolver`** (`laravel/src/PageResolver.php`) — pure class mirroring
  `protocol.ResolveURL` (`protocol/routing.go`) row-for-row: `""`→`pages/index`,
  flat-wins-over-nested, directory index, `.`/`..`-segment traversal → null.
  `resolve()` returns the page's view-relative path (`pages/blog/index`);
  `notFoundView()` is the serving-layer half of Go's `notFoundOr` (the 404 page
  carries a 404 status, so its selection is split out of `resolve()`). Tested by
  `laravel/tests/PageResolverTest.php`, whose rows are copied verbatim from
  `protocol/routing_test.go`.
- **`SiteServer`** (`laravel/src/SiteServer.php`) + **`AssetMime`**
  (`laravel/src/AssetMime.php`) — the fallback body: (a) `{site_root}/public/`
  file (realpath-contained, no dotfiles, no directory listings, mime-typed via a
  package-local port of devdojo's `assetMimeType`), (b) page → real Blade at 200,
  (c) `pages/404.blade.php` → 404, (d) `abort(404)`. GET/HEAD only.
- **Provider wiring** — `PocketknifeServiceProvider::boot()` registers a
  `Route::fallback` only when `config('pocketknife.serve')` (env
  `POCKETKNIFE_SERVE`, default false) AND `site_root` are set. `serve` is the one
  config addition (`laravel/config/pocketknife.php`, which carries the serving
  docs in comments — the package has no README).
- **Rendering mechanism** — pages render via `View::make($dotName)` (not
  `View::file()`), so the `SiteData` `pages.*` composer fires and collections /
  content bind; the view-relative→dot-name mapping matches the conformance
  harness's own `str_replace('/', '.', …)`, so serving and the oracle agree
  byte-for-byte. (A page-path segment with a literal `.` is unsupported, the
  same limitation the conformance mapping carries.)
- **Non-GET verbs** — the fallback is Laravel's standard GET/HEAD `Route::fallback`.
  A POST to a page URL is therefore never served; Laravel returns its standard
  405 (the URI exists for GET only). Our serving handler never runs for non-GET.

**Proof (oracle, Laravel 12):** `oracle/tests/Feature/AppTierServingTest.php`
serves **every oracle-checked, data-free fixture** over real HTTP and asserts the
body is byte-identical to `expected/` — **21 fixtures / 25 URLs** (three-way
agreement: Go engine == Blade render == Blade-over-HTTP). Fixtures with a
case.json `data` override are skipped (the protocol data map has no HTTP
equivalent — `SiteData` binds only the site's own file-sourced data; they stay
covered by `pocketknife:conformance`). Shadowed nested directory-index pages
(flat wins) are covered by the precedence test rather than a direct GET.
`oracle/tests/Feature/ServingSemanticsTest.php` pins 404 semantics (site 404 +
fallthrough), flat-wins precedence, app-route-wins, GET-only (405 on POST), HEAD,
asset serving (mime + asset-wins-over-page + dotfile refusal + no directory
listing + traversal refusal), and the opt-in switch (serve=false registers no
fallback). The render conformance stays green (35/35).
