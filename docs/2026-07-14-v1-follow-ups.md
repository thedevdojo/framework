# Pocketknife v1 — Follow-ups & Open Decisions

v1 shipped 2026-07-14 (final review verdict: READY-WITH-FOLLOW-UPS, zero criticals).
This is the reviewable list of what remains. Items are ordered by weight.

## Decisions (need Tony)

### 1. Unknown directives: strip vs leave-literal — RESOLVED (leave-literal, 2026-07-15)
Previously any directive outside the subset (`@auth`, `@media`, …) rendered as
**nothing** plus an app-tier diagnostic. Real Blade leaves unknown directives
**literal**. The final review found this collided with Tailwind v4 idioms —
`class="@container"`, `@min-md:*` variants, and `@media`/`@keyframes` in inline
`<style>` blocks were all silently deleted from output.
**Resolution (approved, leave-literal):** unknown directives now render **byte-for-byte
literal** in output while keeping the app-tier diagnostic. This restores real-Blade
parity for unregistered directives and unbreaks Tailwind. Implemented in
`blade/scanner/scanner.go` (`Token.Src`), `blade/parser/parser.go` +
`blade/ast/ast.go` (`Unknown.Src`), and `blade/render/render.go` (Unknown case);
the `app-tier-directives` fixture output was regenerated (diagnostics unchanged), a
new oracle-checked `css-at-rules` fixture pins the CSS-at-rule parity, and spec §3.7
now documents leave-literal.

### 2. Go value model: JSON objects behave as PHP arrays, not objects
JSON objects become `map[string]Value` on the Go side (PHP-*array* semantics) but
`stdClass` on the Laravel side. Silent divergences: empty-object truthiness
(falsy Go / truthy PHP), `$emptyObj == null`, `$plans[0]['name']` (works Go /
fatals PHP), missing-member access (silent nil Go / ErrorException PHP — this also
swallows the spec's promise that `$loop->remaining/depth/parent` are app-tier).
**Must be resolved before platform integration.** Options: a distinct object type
in the Go value model, or diagnostics + a spec §3.4 amendment. Start with oracle
probes + fixtures pinning current behavior.

#### Probe memo (2026-07-15, S4) — PINNED, resolution still a Phase 4 gate

Oracle-probed each divergence against real Blade (data shaped exactly as the
`SiteData` contract ships it: `$site` = `json_decode` object → **stdClass**,
collections = arrays of **stdClass**, content items = **objects**). Exact results:

| Case (input) | Real Blade | Go engine (pinned) |
|---|---|---|
| `@if ($site)` with `site.json = {}` | **truthy** — empty `stdClass` is truthy → true branch | **falsy** — empty `map[string]Value` → false branch |
| `$plans[0]['name']` (`[]` on a stdClass item) | **fatal** `Error: Cannot use object of type stdClass as array` (surfaces as `ViewException`, non-zero exit) | works — `map` index read → `Pro` |
| `$site['x']` (`[]` on the stdClass `$site`) | **fatal** — same `Cannot use object of type stdClass as array` | works — missing key → `nil` → `` |
| `$posts[0]->typo` bare (undefined member) | **throws** `ErrorException: Undefined property: stdClass::$typo` (Laravel promotes the E_WARNING; `ViewException`, non-zero exit) | silent `nil` → `` |
| `$posts[0]->typo ?? 'd'` (coalesced) | `d` — `??` uses isset semantics, suppresses the warning | `d` — **agrees** (no divergence) |

Pinned as `"oracle": false` fixtures (`value-model-empty-object-truthiness`,
`value-model-object-array-access`, `value-model-missing-member`); each carries
**zero Go diagnostics** — that is the hazard, the Go engine silently succeeds
where real Blade aborts. The `??` case is included to record that the coalesced
form already matches (only the *bare* member/index access diverges).

Two shapes of divergence:
1. **Wrong-but-quiet output** (empty-object truthiness): both tiers render, bytes
   differ. A `stdClass`-vs-array distinction in the Go value model is the only
   fix; a diagnostic cannot help because there is no obvious error site.
2. **Go succeeds where Blade fatals** (`[]`-on-object, bare undefined member): the
   Go engine must not hand back HTML that real Blade would refuse to produce.

**Recommendation:** introduce a **distinct object type** in the Go value model
(carry JSON objects as an `object`, keep JSON arrays as the list/map array type),
so `->` reads objects, `[]` on an object is an *error* diagnostic, an undefined
member is an *error* (with `??`/isset still short-circuiting), and empty objects
are truthy. Diagnostics-only (keep arrays, emit app-tier on `[]`-of-object and
bare-missing-member) is the lighter fallback but cannot fix the truthiness bytes.
Either way this ships with a spec §3.4 amendment. **Resolution stays a Phase 4
gate** — this memo only pins the current behavior.

### 3. Undeclared-attribute lint warning (spec §3.3 commitment)
Spec says "lint warns on undeclared attributes"; unimplemented (renderer silently
drops them — correct for output parity — and `pocketknife lint` admits the gap).
Prerequisite: a `warning` diagnostic kind (today only `error` | `app-tier`).
Implement, or amend the spec to drop the commitment.

### 4. Oracle: Laravel 11 + permanent Composer audit bypass
`oracle/composer.json` carries `audit.block-insecure: false` because every
installable Laravel 11 release is advisory-flagged (11 is past its patch window).
**Recommendation:** bump the oracle to Laravel 12 or 13 — the package already
supports `^11|^12|^13`, it removes the bypass, and 13 matches `default/`.

### 5. GitHub repo + first CI run
The workflow (`.github/workflows/ci.yml`) is authored and every command verified
locally, but the repo has no remote — CI has never executed on GitHub. Creating
`thedevdojo/pocketknife` + one green Actions run closes the last sliver of the
spec §8 exit criteria.

### 6. Spec amendments batch (mechanical, post-decision)
- §3.1/3.2 newline model: oracle showed echoes/comments PRESERVE a following
  newline; block directives EAT one (spec's hypothesis was inverted).
- §3.5: layouts are components — they receive data via bound props, see `$site`
  only (not "collections bound in pages and layouts").
- §3.2: loop variables don't leak past `@endforeach` (documented divergence);
  map iteration is sorted-key order.
- §3.3: `@props` on a page / not-first-in-file → error (stricter than real Blade).
- §4: promised root `pocketknife.Compile` facade doesn't exist (real API is
  `render.Compile`) — add the facade or amend.
- §2: promised routing-table fixture doesn't exist — add or amend.
- §10: record the `\B@` word-boundary rule and the `@{{`-exemption (oracle-probed).

## Tracked SHOULD-FIX-SOON (small, no decision needed)

- **Named-slot names aren't camelCased** — `<x-slot:foo-bar>` binds an unreachable
  `$foo-bar`. Oracle-probe real Blade first, then camelCase or diagnose.
- **Fixture harness matches diagnostics by presence, not exact set** — stray diags
  could accumulate unseen in oracle-exempt fixtures.
- ~~**Invalid-UTF-8 escaping unverified vs real `e()`**~~ — RESOLVED (2026-07-15,
  S4). Oracle probes found a real divergence: Go's per-byte `utf8.DecodeRune` loop
  emitted one U+FFFD **per byte**, but PHP's `htmlspecialchars(ENT_SUBSTITUTE)`
  collapses an ill-formed subsequence (lead + its `0x80–0xBF` continuations, up to
  the lead's declared length, **plus one** terminating `0xC0`/`0xC1`/`0xF5–0xFF`
  byte when the run stops early) into a **single** U+FFFD — so `E2 82` → one,
  `C3 FF` → one, `FF FF` → two, and overlong/surrogate full-length runs still
  collapse to one. Fixed `Escape` (`phpSubpartLen` in `blade/render/echo.go`),
  pinned by `TestEscapeInvalidUTF8` + the oracle-checked `escape-invalid-utf8`
  fixture, and documented in spec §3.1. (The trailing-invalid-lead absorption was
  caught in review — the first cut stopped at ANY non-continuation byte.)
- ~~**No fixture echoes bool/int frontmatter values**~~ — RESOLVED (2026-07-15,
  S4). The oracle-checked `frontmatter-types` fixture echoes bool/int/float
  frontmatter through `{{ }}` and confirms both tiers agree byte-for-byte:
  `42`/`-7`/`0`, `3.14`/`2.5`, `3.0` → `3`, `true` → `1`, `false` → `` (empty).
- **Missing components / missing `@vite` assets aren't added to deps** — a
  render-on-save host won't refresh when the user creates the missing file.
- **No render budget** — nested `@foreach` over protocol data is unbounded; either
  document that hosts must wrap the binary in limits, or add caps.
- **Four near-identical quote/depth mini-scanners** (parser ×2, props ×2) with
  inconsistent depth-clamping — convergence candidate.
- **Package hygiene:** no `publishes()` for config; `minimum-stability` noise in
  `laravel/composer.json`; `View::addLocation` precedence (app views shadow site
  views) needs a decision for toggle-to-app.
- ~~**`word@@word` renders divergently**~~ — RESOLVED with decision #1 (2026-07-15):
  `a@@b` now renders `a@@b`, matching real Blade (the trailing `@b` is a literal
  unknown directive).

## Known, endorsed divergences (documented, not defects)

Loop vars don't leak; map iteration sorted; `@props` page-level restriction;
components see `$site` only; undefined vars are compile errors; unknown directives
render literally + diagnosed app-tier (decision #1, 2026-07-15); classic-engine
whitespace equivalence is NOT a guarantee (only Go-vs-real-Blade parity is).

## Found during Phase 2 (2026-07-14, aria conversion)

- ~~**Component leading-whitespace parity gap**~~ — RESOLVED (2026-07-15, S4).
  Oracle probe pinned the exact mechanism: real Blade's `PhpEngine::evaluatePath`
  returns `ltrim(ob_get_clean())` for **every** rendered view, so a view's output
  has its LEADING whitespace stripped and trailing bytes preserved. Charset is
  PHP's default `ltrim` mask `" \t\n\r\0\x0B"` — space, tab, LF, CR, NUL, vertical
  tab; **form feed (0x0C) is NOT trimmed** (probed: a leading `\f` survives).
  **Surprise:** this applies to PAGE entries too, not only components — a page
  opening on a blank line or indentation was silently diverging. Fixed both:
  `phpLtrim` (shared `isPhpTrim` mask) is applied to component output in
  `blade/render/component.go` and to entry-page output in `blade/render/render.go`.
  Oracle-checked fixture `component-leading-whitespace/` locks all of it (blank
  line after `@props`, leading indentation, form-feed preservation at both the
  component and page boundary). Two unit tests that asserted a pre-parity leading
  space on page output (`TestIfConditionError`, `TestAttributesIfCondAppTier`)
  were corrected. Templates authored to start with non-whitespace are unaffected;
  the aria workaround can now be dropped.
- **`@props` rejects array defaults** (`@props(['menu' => []])`) — array-holding
  props must be declared bare. Either teach the restricted literal parser
  empty-array defaults or document the bare-declaration idiom in the spec.
