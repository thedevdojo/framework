# Pocketknife Phase 4a — App-Tier Serving

**Design spec · 2026-07-16 · Status: approved direction (Tony: build the app-tier runtime; boundary settled against the devdojo/sites working plan) · Repo: pocketknife (main)**

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
  `/about` → `pages/about.blade.html` else `pages/about/index.blade.html`
  (flat wins), nested paths, traversal-guarded. A PHP `PageResolver` in the
  package mirrors the table; its tests assert the same rows the Go side pins.
- **Rendering**: the resolved page renders as a real Blade view with the
  package's existing bindings ($site, collections, content, @vite static
  projection when no build exists). No editor markers, no diagnostics — this
  is real Laravel; real Blade errors behave like any app error.
- **404**: unresolved path + site has `pages/404.blade.html` → render it with
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
