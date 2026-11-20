# Collections become global scope (like $site)

> **A note on names:** this was written when the project was called Pocketknife. It is now DevDojo Framework (`github.com/thedevdojo/framework`, binary `devdojo`). The names below are left as they were written.

**Date:** 2026-07-28
**Status:** Approved (Tony) — implementation with the devdojo guardrails plan
**Origin:** devdojo site `1d3xju22` — a nav component's repeater edits
`collections/links.json` in the builder, but nothing can render it: the nav is
instantiated in a layout, and neither layouts nor components see collections.
The 2026-07-14 spec's data table claims collections are "bound in pages and
layouts" — the engine (and the Laravel tier) never implemented the layout
half. Decision: rather than special-casing layouts, collections (and content
collections) become **global**, exactly like `$site`.

## The contract change

| Binding | Before | After |
|---|---|---|
| `$site` | Everywhere (View::share parity) | unchanged |
| `$<collection>` (collections/*.json) | Pages only | **Everywhere** — pages, layouts, components |
| `$<dir>` (content/*/*.md) | Pages only | **Everywhere** |
| `Options.Data` (protocol `data`) | Pages only, wins on collision | unchanged — still page-only |

Precedence inside a component scope, low → high:

1. shared bindings (collections + content — the ORIGINAL file data, never
   `Options.Data`-overridden)
2. `$site` (so a collection named `site` can never displace the site object,
   in a component **or on a page** — see the correction below)
3. a **passed** attribute (`:links="…"` / `links="…"`), which arrives as the
   component view's per-view data and therefore beats shared data
4. declared `@props` **defaults**, which fill in only where the name is still
   null — a default can **NOT** shadow a same-named collection, but it DOES
   fill in behind a passed null
5. `$slot` / named slots

### Correction 2026-07-28 (implementation, settled against the oracle)

The draft's precedence list originally had three ranks, with rank 3 reading:

> declared `@props` (a passed attribute or default **shadows** a same-named
> collection — this is the footgun the devdojo audit teaches: a component with
> `@props(['links' => null])` and no binding sees null, not the collection)

and the draft also asserted, in a paragraph now deleted, that "Page scope is
untouched: `buildSiteData`'s bindings (site → collections → content →
`Options.Data`, later wins) already model it."

Both claims were wrong about real Blade. Each was probed directly through
`php artisan pocketknife:render`, and the engine follows real Laravel per the
dual-oracle rule. The list above (ranks 3–5) is the corrected contract:

- **A declared `@props` default does not shadow a collection.** Blade's
  `compileProps` emits `$x = $x ?? $default` (and the same `??` form for a
  passed attribute), and shared data is already `extract`ed into an anonymous
  component's scope before `@props` runs. So a component with
  `@props(['links' => null])` and `collections/links.json` present renders the
  COLLECTION, not null. Only an explicitly passed attribute overrides it —
  because a passed attribute is per-view data, which `Factory::gatherData`
  merges *over* shared data. The devdojo-side audit and prompt work must teach
  this shape, not the "default shadows" footgun the draft assumed.
- **A passed attribute overwrites shared data even when its value is null**,
  and the default then fills the hole. `<x-nav :links="null"/>` against a
  component declaring `@props(['links' => 'D'])` renders `D`, not the
  collection: the per-view merge nulls the binding, then the `??` defaults pass
  fires. So passing null is the one way a caller *can* get back to the default.
  Pinned by `fixtures/component-props-null-vs-default`.
- **Page scope is NOT entirely untouched.** Sharing `site` last means a
  collection named `site` stops shadowing `$site` on pages too (it previously
  did, via the page-only binding order). `buildSiteData` therefore binds
  collections → content → `site` → `Options.Data`, matching
  `SiteData::share`'s order. Pinned by
  `fixtures/data-site-collection-collision`.

What the deleted paragraph got right, and still holds: `Options.Data` wins on
the page, so dynamic-page entry overrides (`data: {collection: entry}`) still
shadow the collection **in the page only** — a component reading `$posts` sees
the full collection, identical to real Blade's share-vs-view-data behavior.

## Both tiers move together (parity by construction)

- **Go engine:** `buildSiteData` additionally exposes the collections+content
  map (pre-`Options.Data`, no `site` key) as `shared`; `renderCtx` carries it
  beside `site`; `buildComponentScope` seeds the scope in the precedence
  order above. No protocol change.
- **Laravel tier** (`laravel/src/SiteData.php`): the `composer('pages.*')`
  binding for collections/content is replaced with `$factory->share()` per
  binding, sharing collections/content **first** and `site` **last** (so
  `site` wins in shared data, mirroring precedence rule 2). Per-view data
  (the protocol `data` map rendered into the page view) already overrides
  shared data in real Blade — that is precisely the page-scope collision rule.

## Conformance

- Revise `fixtures/component-scope-isolation`: isolation's remaining meaning
  is "page-local data does not leak" (`Options.Data` keys, loop locals) —
  not collections.
- New fixtures covering: a component reading a collection directly and a
  content collection (`component-reads-collection`); a layout binding a
  collection into a child component via `:links="$links"`
  (`layout-binds-collection`); the props/collection interaction
  (`component-props-vs-collection` — a `@props` default losing to the
  collection, a passed attribute winning); the passed-null/default interplay
  (`component-props-null-vs-default`); and `$site` outranking a
  `collections/site.json` on page and in component
  (`data-site-collection-collision`).
- `make conformance` (dual-oracle, byte-identical against real Laravel) must
  pass — the oracle app binds through the same `SiteData`, so both tiers
  change in one commit.

## Ripples

- `docs/specs/2026-07-14-pocketknife-design.md` §3.5 data table rows for
  collections/content change to "Available everywhere" with the shadowing
  note (edited in place, with a dated amendment marker).
- devdojo: `RepeaterBindingAudit` gains the direct-collection-read shape;
  prompt/rules teach collection-first navs; `POCKETKNIFE_BINARY` consumers
  need the rebuilt binary (`go build -o bin/pocketknife ./cmd/pocketknife`).
- Any `SiteData`-replica stubs (devdojo App Mode generated stubs, the
  sites connect package) must mirror the composer→share change.
