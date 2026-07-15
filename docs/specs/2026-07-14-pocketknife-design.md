# Pocketknife — the portable Blade engine

**Design spec · 2026-07-14 · Status: approved (Tony Lea) · Scope: v1**

> **Pocketknife** — *the portable Blade.* A Go engine that statically renders a closed
> subset of Laravel Blade, paired with the Laravel package that makes the identical
> files render in a real Laravel app. One repo, two renderers, one contract.

---

## 0. Summary

Pocketknife is a single self-contained repository (`~/devdojo/pocketknife`,
module `github.com/thedevdojo/pocketknife`) containing:

| Part | What it is |
|---|---|
| `blade/` | The Go engine: scanner, parser, renderer, expression evaluator for the Blade subset. Stdlib-only core. Never `eval`s, never panics on user input, byte-faithful to user HTML. |
| `laravel/` | The `devdojo/pocketknife` Composer package: makes the same files render in real Laravel (`View::addExtension('blade.html', 'blade')`, data bindings, component paths, static-projection Vite). |
| `fixtures/` | **The contract.** Language-agnostic conformance cases, each rendered by the Go engine *and* by real Blade, byte-diffed. |
| `oracle/` | A minimal Laravel skeleton that consumes `laravel/` and runs the fixtures through real Blade in CI. Doubles as a local preview server for converted templates. |
| `cmd/pocketknife/` | CLI: `compile` (JSON stdin/stdout embedding protocol), `lint`, `version`. |

**Why it exists (product rationale).** DevDojo Sites and DevDojo Apps converge into one
product: a dial from static website to Laravel application. Blade subset is the static
tier's language; real Blade is the app tier's language — and they are the *same syntax
on the same files*. The static tier must serve multi-tenant from shared infrastructure,
and real Blade compiles to arbitrary PHP, so real Blade can never run there. A no-eval
Go renderer for a defined Blade subset is the only safe way to put Blade syntax in the
static tier. Constructs outside the subset are not errors — they are **tier signals**:
"this needs your site running as an app," which is the upgrade prompt.

**The one rule that keeps this honest:** every construct the static tier supports must
render **byte-identically** through real Blade given the `laravel/` package. Real
Laravel is the conformance oracle. Anything we cannot make agree, we do not ship in the
subset.

---

## 1. Positioning vs. sitebuilder

- **sitebuilder is untouched.** It keeps the classic Static grammar and all its
  machinery (dev server, build, Tailwind, markdown, the `site compile` protocol),
  serving div, static.devdojo.com, and every existing Site. Its `blade-dev` branch is
  abandoned.
- **No parity contract exists between the two dialects.** Classic and Blade are
  different languages; nothing semantic can drift between the repos. The pair that must
  never drift — Pocketknife's Go renderer vs. real Blade — lives in *this* repo with the
  fixtures that bind it.
- **Duplication is plumbing, not engine.** The platform consumes engines only through
  the JSON compile protocol; it never uses dev servers or CSS builds (user sites ride
  the Tailwind Play CDN). Pocketknife therefore needs only: engine + vfs + protocol.
- **The `site` binary is not deployed in production anywhere** (confirmed 2026-07-14),
  so there is no dual-binary routing or classic-dialect conversion burden: the
  platform-integration phase points the engine seam (`EngineManager::compilerFor()`)
  directly at `pocketknife`. Classic remains a local/dev concern for div and the
  static CLI only.

---

## 2. Project structure (what a Blade-dialect site looks like)

The tree mirrors a Laravel application — deliberately. "A site is a slice of a Laravel
app" is literally true: toggle-to-app moves zero files.

```
my-site/
├── public/                               # static assets, verbatim (images live here: public/images/)
└── resources/
    ├── views/
    │   ├── pages/
    │   │   ├── index.blade.html          # → /            (Folio's routing convention)
    │   │   └── about.blade.html          # → /about
    │   └── components/
    │       ├── layouts/
    │       │   └── main.blade.html       # <x-layouts.main> … {{ $slot }}
    │       └── sections/
    │           └── hero.blade.html       # <x-sections.hero heading="…"/>
    ├── css/
    │   └── site.css                      # Tailwind v4 entry (@import "tailwindcss")
    ├── js/
    │   └── site.js                       # reserved; JS entries are app-tier in v1
    └── data/
        ├── site.json                     # global data → $site
        ├── collections/
        │   └── plans.json                # JSON array → $plans
        └── content/
            └── post/*.md                 # markdown items → $post collection
```

**File extension: `.blade.html`** (the Statamic `.antlers.html` pattern). It carries
both truths — Blade syntax, HTML document. HTML highlighting in Monaco and GitHub, no
PHP-execution footgun on user content in exports, and real Laravel compiles it via one
line in the package: `View::addExtension('blade.html', 'blade')`. Eject-to-plain-Laravel
can mechanically rename to `.blade.php` for users who want zero-dependency views.

**Routing** (same rules as the classic engine; the app-tier Folio mount must match):
`/` → `pages/index.blade.html`; `/about` → `pages/about.blade.html`, else
`pages/about/index.blade.html` (flat wins); 404 → `pages/404.blade.html` if present.
v1 exercises routing only through explicit protocol `entries`; a routing fixture pins
the table for the later phases.

There is no root config file in v1 — the compile protocol carries all options.

---

## 3. The dialect — "static-tier Blade"

A **closed subset** of real Blade. Everything inside the subset renders identically in
both tiers. Everything outside it is *valid Blade classified as app-tier*, reported as
a structured diagnostic — never a silent drop, never a crash.

### 3.1 Output

| Construct | Behavior |
|---|---|
| `{{ expr }}` | Escaped output. Exactly Laravel's `e()`: `htmlspecialchars($v, ENT_QUOTES \| ENT_SUBSTITUTE, 'UTF-8')` — `& < > " '` → `&amp; &lt; &gt; &quot; &#039;`; invalid UTF-8 → U+FFFD. |
| `{!! expr !!}` | Raw output. |
| `{{-- … --}}` | Removed entirely (Blade strips comments pre-compile; surrounding whitespace is preserved). |

**Stringification follows PHP `echo`:** `null`/`false` → `""`, `true` → `"1"`, floats
render shortest-round-trip (PHP `serialize_precision=-1`: `1.0` → `"1"`, `2.5` →
`"2.5"`). Echoing an array or object is a diagnostic **error** (real Blade throws a
`TypeError` in `e()`; we catch it at compile time).

**Invalid-UTF-8 substitution (oracle-verified 2026-07-15):** the `ENT_SUBSTITUTE`
replacement is done PHP's way, not Go's default. At an ill-formed offset,
`htmlspecialchars` consumes the lead byte plus its continuation bytes (`0x80–0xBF`)
up to the length the lead declares (`0xC2–0xDF` → 2, `0xE0–0xEF` → 3, `0xF0–0xF4` →
4; any other lead — a stray continuation, an overlong `0xC0`/`0xC1`, or an
out-of-range `0xF5–0xFF` — is length 1) and emits a **single** U+FFFD for the whole
run. Overlong and surrogate sequences that reach full length still collapse to one
U+FFFD (they are invalid, not preserved). So `E2 82` → one U+FFFD (not two, which is
what a naive per-byte `utf8.DecodeRune` loop yields); `FF FF` → two. `Escape` in
`blade/render/echo.go` implements this via `phpSubpartLen`; pinned by the
`escape-invalid-utf8` fixture and `TestEscapeInvalidUTF8`.

### 3.2 Control flow

- `@if (expr)` / `@elseif (expr)` / `@else` / `@endif`
- `@foreach ($items as $item)` / `@foreach ($items as $key => $item)` / `@endforeach`
- `@break` / `@continue`, with the optional condition argument (`@break($loop->iteration == 3)`)
  — this is also how templates express "show the first N," since the dialect has no
  method calls
- `$loop` inside loops: `index`, `iteration`, `first`, `last`, `count`, `even`, `odd`.
  (`remaining`, `depth`, `parent` are app-tier in v1.)

**PHP output fidelity:** Blade directives compile to `<?php … ?>` and PHP consumes
exactly one newline after a closing `?>` — so a directive on its own line leaves no
blank line in output. The Go renderer replicates this. This class of quirk is exactly
what the oracle exists to police.

### 3.3 Components

- `<x-sections.hero heading="…"/>` — self-closing or paired. Dots are directory
  separators under `resources/views/components/`; **exact file match only**
  (`sections/hero.blade.html`; index-file resolution à la `hero/index.blade.php` is not
  in v1 — lint reports it app-tier).
- `@props(['heading' => 'Default headline'])` — a literal array with scalar defaults.
  Declared props become variables; **defaults live in the template** via `@props` (and
  `??` for `@include`-less inline defaults), not injected from companion YAML. This
  retires the classic engine's `withDefaults()` pre-pass: the default is real Blade
  semantics and works identically in both tiers.
- Static attributes pass strings; **bound attributes** `:plans="$plans"` pass evaluated
  expressions (needed to hand collection data into sections).
- **Undeclared attributes are dropped.** Real Blade routes them into `$attributes`; the
  dialect forbids `$attributes`, and a component that never echoes `$attributes`
  produces the same output as one where the attribute was dropped — so *outputs agree*.
  Lint warns on undeclared attributes to catch typos.
- `{{ $slot }}` — default slot. **Named slots** are supported: `<x-slot:title>…</x-slot>`
  (layouts need them; Designer Studio's Blade export already uses them).
- Component recursion depth cap: **16**, exceeded → diagnostic error (real Blade would
  recurse to a crash; fixtures stay under the cap).

Layouts are ordinary anonymous components (`<x-layouts.main>` wrapping page content in
`{{ $slot }}`) — no custom layout construct exists in this dialect.

### 3.4 Expressions

Grammar (evaluated by `blade/expr` — hand-written tokenizer + recursive descent,
never `eval`, semantics ported from the proven classic evaluator):

- Variables `$name`; property access `$post->title`; array access `$post['key']`,
  `$items[0]`
- `??` (null-coalescing) — also the sanctioned defaults pattern
- Comparison `== != < > <= >=` (non-associative), boolean `&& || !`, parentheses
- Literals: `'str'`, `"str"`, ints, floats, `true`, `false`, `null`
- **PHP truthiness** (`0`, `""`, `"0"`, `null`, `[]` falsy; `"false"` truthy) and
  **PHP 8 loose equality** (`'1' == 1` true, `'abc' == 0` false)

**Not in the subset** (app-tier diagnostics): function/method calls, `?->`, string
concatenation, assignments, closures, `$attributes`, `@php`, `@include`, `@auth` and
every other directive not listed above.

**Undefined variables are compile-time errors.** Real Blade throws at runtime on
undefined variables; rendering them as silent empties would be exactly the semantic
drift the oracle exists to catch. The engine statically knows the full data context, so
it reports file/line/col. As in PHP, `??` suppresses the undefined-variable error for
its left-hand operand — `{{ $tagline ?? 'Welcome' }}` is always legal.

### 3.5 Data bindings (the contract `laravel/` mirrors)

| Source | Binding | Notes |
|---|---|---|
| `resources/data/site.json` | `$site` (object) | Available everywhere. Namespaced so it cannot collide with collections. |
| `resources/data/collections/plans.json` | `$plans` | JSON array; bound in pages and layouts. Components receive data via props only. Order is as authored in the file. |
| `resources/data/content/post/*.md` | `$post` | Items carry frontmatter fields plus `content` (rendered HTML) and `link` (route). Ordered by filename ascending. Rendered inline via `{!! $item->content !!}`. |

Markdown-driven *page routing* (classic's nearest-template walk) is **not in v1** —
content collections render inline; detail-page routing arrives with the Folio phase.

### 3.6 Assets: `@vite`

`@vite(['resources/css/site.css'])` is in the subset, rendered as its **static
projection**: the Tailwind v4 Play CDN script plus the entry's contents inlined as
`<style type="text/tailwindcss">` — the same output contract user sites have today.
On the Laravel side, the package swaps the `Vite` binding for a static-projection
implementation while the app has no built assets; when a user upgrades and runs real
Vite, the package steps aside (config flag). Same line, correct in both worlds.
JS entries in `@vite` are app-tier in v1 (static sites use plain `<script>` tags/CDNs).

### 3.7 Diagnostics

Every diagnostic carries `file`, `line`, `col`, `message`, and a `kind`:

- **`error`** — malformed input: unclosed directives, bad expressions, undefined
  variables, echoing non-scalars, missing components, recursion cap.
- **`app-tier`** — valid Blade outside the subset, with the construct named
  (`@auth`, `function call`, `$attributes`, …). Hosts render these as "this needs your
  site running as an app," and the AI lint loop treats them as tier signals, not
  failures.

**Unknown directives render literally.** A directive outside the closed subset is
written to output **byte-for-byte** (its exact source, casing and inner whitespace
preserved) alongside its `app-tier` diagnostic — never dropped. This matches real Blade,
which leaves *unregistered* directives literal, and keeps inline CSS at-rules
(`@media`, `@keyframes`, `@container`) and boundary `@word`s in prose intact instead of
corrupting them. (Registered-but-app-tier directives like `@auth`/`@php` render literally
here but would *compile* in real Blade, so any page carrying an app-tier diagnostic is
outside the byte-parity promise — the diagnostic is the signal to run it at the app tier.)

Missing components additionally emit a byte-stable output marker —
`<!-- pocketknife:missing component "sections.hero" -->` — the same marker-contract
pattern hosts already rely on with the classic engine (distinct `pocketknife:` prefix).

The engine **never panics** on user input; fuzz targets enforce this.

---

## 4. Go engine architecture

```
pocketknife/
├── cmd/pocketknife/      # CLI: compile, lint, version — hand-rolled args, no framework
├── blade/
│   ├── scanner/          # source → tokens (text spans, directives, echoes, x-tags) w/ line+col
│   ├── parser/           # tokens → AST; depth-matched pairs; real attribute lexing
│   ├── ast/              # node types; every node carries file/line/col
│   ├── render/           # AST + data context → output; component/slot/props resolution
│   └── expr/             # the expression evaluator (§3.4)
├── vfs/                  # resolver interface: disk impl (CLI/oracle), in-memory impl (protocol)
├── protocol/             # JSON stdin/stdout compile mode (§6)
├── content/              # markdown pipeline for content collections (goldmark)
├── laravel/              # the Composer package (§5)
├── oracle/               # conformance skeleton (§7)
├── fixtures/             # conformance suite (§7)
└── docs/specs/           # this document
```

Conventions carried over from sitebuilder, deliberately:

- **AST, not regex; byte-faithful or bust.** The scanner recognizes only the dialect;
  everything else is an opaque text span emitted verbatim. No HTML normalization, ever.
- **Core stays stdlib-only** (`blade/*`, `vfs/`, `protocol/`). The only approved
  external dependency is `goldmark` in the outer `content/` package.
- **Fixture-first development.** A behavior without a fixture doesn't exist.
- Table-driven tests, Go fuzz targets on scanner/parser, `gofmt`/`go vet` clean.
- Public Go API kept minimal: `pocketknife.Compile(fs, entries, opts) → files, deps, diagnostics`.

---

## 5. The `laravel/` package (`devdojo/pocketknife`)

The Laravel-side half of the contract. Its service provider:

1. `View::addExtension('blade.html', 'blade')`
2. Registers the site's view roots: `resources/views` for pages/views, anonymous
   component path for `resources/views/components`
3. Binds the data contract (§3.5): `$site`, collections, and content collections shared
   into views (view composers reading `resources/data/`)
4. Swaps in the static-projection `Vite` implementation when the app has no built
   assets (§3.6)

Config `config/pocketknife.php`: site root path, static-projection toggle. Supports
Laravel `^11.0|^12.0|^13.0` (the platform is 11; the app template is 13). In later
phases this same package grows the Folio page mounting and the toggle-to-app wiring —
the oracle is its first consumer, which is what guarantees the two tiers can never
disagree about what the files mean.

Wiring into the platform (later phase): one additional Composer `path` repository entry
pointing at `../pocketknife/laravel`. Publishing to Packagist, if Pocketknife goes
public, uses a subtree split (the `illuminate/*` monorepo pattern).

---

## 6. The embedding protocol (`pocketknife compile`)

One JSON document on stdin → one on stdout, deliberately the same shape as
sitebuilder's protocol so host wrappers transfer directly:

```jsonc
// stdin
{
  "files":   { "resources/views/pages/index.blade.html": "…", "resources/data/site.json": "…" },
  "data":    { "site_name": "My Site" },        // optional; merged over $site
  "entries": ["resources/views/pages/index.blade.html"],   // omitted → all pages
  "options": { "url": "relative" }
}

// stdout
{
  "engine":      "0.1.0",
  "files":       { "index.html": "<rendered>" },
  "deps":        { "index.html": ["resources/views/components/layouts/main.blade.html"] },
  "diagnostics": [ { "file": "…", "line": 12, "col": 3, "kind": "app-tier", "construct": "@auth", "message": "…" } ]
}
```

Differences from sitebuilder's protocol: `errors` becomes `diagnostics` with the `kind`
and `construct` fields (§3.7). `deps` keeps the render-on-save contract. Tokens and
provider keys never pass through the engine; it sees only site source files.

---

## 7. Fixtures and the oracle

**Fixture format** (proven in sitebuilder):

```
fixtures/<case-name>/
├── input/**            # the site source tree
├── expected/**         # exact rendered output, byte-for-byte
└── case.json           # entries, data, options, expected diagnostics
```

**The oracle** is a minimal committed Laravel skeleton in `oracle/` (vendor
gitignored) that consumes `laravel/` and exposes a render script: point it at a
fixture's `input/`, render the entries through **real Blade**, byte-diff against both
`expected/` and the Go engine's output. Cases exercising engine-only behavior (e.g.
diagnostics for app-tier constructs) mark themselves oracle-exempt in `case.json`.

CI (GitHub Actions): a Go job (`go test ./...`, fuzz smoke, vet) and a PHP job
(composer install in `oracle/`, run the conformance diff). Locally: `make conformance`
when PHP is available; the Go suite alone otherwise.

**The oracle doubles as the dev preview**: `php artisan serve` in `oracle/` pointed at
a template directory serves converted templates through real Blade in a browser — the
v1 substitute for a dev-server CLI.

---

## 8. v1 scope

**In:**

- The dialect exactly as specified in §3 — nothing more
- The Go engine (§4), `laravel/` package (§5), protocol (§6), fixtures + oracle + CI (§7)
- `pocketknife lint`: parse + classify a tree, print diagnostics (the future AI-loop seam)
- **2–3 templates hand-converted** in `~/devdojo/templates` (branch `blade-dev`):
  `blank`, one landing, one multi-page site; each `template.json` gains
  `"dialect": "blade"`. Hand-conversion is deliberate — it's how we learn the idioms
  and stress the grammar before freezing it.

**Out (explicit):**

- Any devdojo/platform change: `EngineManager` routing, Sites UI, Monaco mapping,
  SitePrompt, section-library migration, docs
- The classic→Blade transpiler (it earns its keep against the 289-section library in
  the migration phase)
- App boot: Folio mounting, auth toggle, php-wasm integration
- `$attributes`, `@include`, `@php`, function calls, `?->`, string concat, repeater
  fields, markdown page routing, JS bundling, index-file component resolution
- A dev-server CLI, WASM build, Packagist publishing

**Success criteria:**

1. Every fixture renders byte-identically through the Go engine and the oracle in CI.
2. The converted templates render byte-identically on both renderers.
3. Fuzzing produces zero panics.
4. `pocketknife lint` on a real-world Blade file (e.g. from `default/`) produces clean,
   accurate app-tier diagnostics — the "tier gate" demo.

---

## 9. Future phases (direction, non-binding)

1. **Platform integration** — the engine seam points at the `pocketknife` binary
   (no dual-binary routing; sitebuilder was never deployed); builder Monaco +
   lint surfaces `app-tier` as upgrade prompts.
2. **Library migration** — classic→Blade transpiler; 289 sections + templates monorepo;
   SitePrompt and the visual tooling (BlockParser addressing moves to `<x-*>` tags);
   companion YAML shrinks to types/labels (defaults live in `@props`).
3. **App tier** — `laravel/` grows Folio mounting; toggle-on-auth boots the same tree
   in the `default/` skeleton on php-wasm or a server.
4. **Portable** — WASM build for in-browser preview; OSS release (MIT, matching DevDojo
   convention) with Packagist subtree split.

---

## 10. Decision log

| Decision | Choice | Why |
|---|---|---|
| Where the engine lives | Self-contained `~/devdojo/pocketknife`; sitebuilder untouched | Platform only consumes the compile protocol, so duplication is ~small plumbing; no semantic contract exists between dialects; the legacy engine stays frozen and retires with the classic grammar |
| Go + PHP in one repo | Yes — engine, package, fixtures, oracle together | The same-grammar pair (Go vs real Blade) and its conformance suite version atomically; cross-repo contract drift becomes structurally impossible |
| File extension | `.blade.html` | Blade syntax + HTML document (the Antlers pattern); Monaco/GitHub highlighting; no PHP-execution footgun; one-line `addExtension` on the Laravel side |
| Folder structure | Mirror Laravel (`resources/views/pages`, `components/layouts`, `components/sections`) | Pages align with Folio's convention; layouts become real component layouts; toggle-to-app moves zero files |
| Undefined variables | Compile-time error | Real Blade throws at runtime; silent empties would be invisible drift; `??` is the sanctioned default pattern |
| Named slots / `$attributes` | Named slots in; `$attributes` out | Layouts need named slots (Studio's export uses them); dropping undeclared attrs is output-equivalent to real Blade when `$attributes` is never echoed |
| Defaults | In-template (`@props`, `??`), not companion YAML | Real Blade semantics, works in both tiers with zero magic; retires the `withDefaults()` pre-pass |
| "Show first N" | `@break($loop->iteration == N)` | Real Blade, avoids method calls/`orderBy` attributes in the dialect |
| Conformance | Real Laravel as oracle, byte-diff in CI | The only thing that keeps "subset of Blade" true instead of drifting into a dialect |
| Rollout | Validate first (2–3 templates), migrate the library later | Freeze the grammar against real templates before spending the 289-section blast radius |
| Templates | Hand-convert in v1 | Learning the idioms is the point; the transpiler belongs to the migration phase |
