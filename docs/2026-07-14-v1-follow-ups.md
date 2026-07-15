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
- **Invalid-UTF-8 escaping unverified vs real `e()`** — one oracle probe fixture
  settles whether U+FFFD substitution matches Laravel.
- **No fixture echoes bool/int frontmatter values** — a frontmatter-types fixture
  would pin the PHP scalar parser (which has no unit tests).
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

- **Component leading-whitespace parity gap:** real Blade trims the *leading*
  whitespace of an anonymous component's rendered output; the Go engine does
  not. Oracle byte-diffs caught it on components whose files start with a
  blank line after `@props(...)`. Fix in `blade/render/component.go` (probe
  real Blade's exact trim behavior first), add a fixture, then un-workaround
  the affected templates (aria's posts-loop / about-experience were authored
  to start with non-whitespace).
- **`@props` rejects array defaults** (`@props(['menu' => []])`) — array-holding
  props must be declared bare. Either teach the restricted literal parser
  empty-array defaults or document the bare-declaration idiom in the spec.
