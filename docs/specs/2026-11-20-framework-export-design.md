# Framework export: a block or a page as React, Vue, Svelte, Alpine or Blade

**Design spec · 2026-11-20 · Status: draft, reviewed once, waiting on Tony Lea · Scope: v1**

> Pick a section or a page on the canvas, choose a framework, and get code a
> developer would be happy to have written: real props with the block's
> defaults, real loops, typed from the block's `.yml`. Plus one small new rule
> for the language: a block owns its JavaScript, in a `<Script>` tag.

This document covers three repositories. The engine work (most of it) is here
in `framework/`. The token API is in `devdojo/`. The menus and the dialog are
in `app.devdojo/`.

---

## 1. Why

DevDojo's promise is that the code is real and the person owns it. Today a
site leaves DevDojo as static HTML, as a Blade site, or through the app's
**Copy as Code**, which works from the *rendered* page: every word is baked
in, a list of six cards is six copies of the markup, and nothing is a prop.

A block's source already says more than that. `<Props>` names what it accepts
and the defaults, `<Foreach>` says what repeats, and the companion `.yml` says
what each value *is* (text, a link, a picture, a list of rows). Exporting from
the source turns that into a component a React, Vue, Svelte, Alpine or Laravel
developer can drop into their own project and keep building on.

Tony's words: "this could really help take DevDojo sites to the next level."

## 2. What exists today (read before planning)

**The language is small and closed, and one parser reads both spellings.**
`blade/scanner` (Blade) and `blade/htmlscan` (Studio syntax, `.html`) emit the
same tokens; `blade/parser` builds one tree (`blade/ast`: `Text`, `Echo`, `If`,
`Foreach`, `LoopJump`, `Component`, `Props`, `Vite`, `ClassDir`, `StyleDir`,
`Unknown`). Expressions have their own tree in `blade/expr` (variables, member
and index reads, `!`, comparisons, `&&`, `||`, `??`, text joined with `.`,
array literals). No math, no ternary, no function calls. See
`docs/studio-syntax.md`.

**The tree does not know HTML.** A `Text` node is an opaque run of bytes.
`<a href="{{ link.url }}">` is `Text` + `Echo` + `Text`. Anything that needs to
know "this echo sits inside an attribute of an `<a>`" has to read the HTML
itself. This is the largest piece of new engine work in this design.

**The engine does not read `.yml`.** The platform does
(`devdojo/app/Services/Sites/Sections/SectionSchema.php`). Field types: `text`,
`textarea`, `url`, `image`, `select`, `color`, `number`, `range`, `toggle`,
`repeater` (with `sub_fields`, which may also be `richtext`). Defaults live in
`<Props>`; the `.yml` says which props are editable and how. A repeater's rows
come from data (`collections/*.json`, or `site.json` through `source: site.x`),
never from the tag.

**Convert is the precedent.** `devdojo convert` (`blade/convert`) reprints
a site in the other syntax, "exact, or not at all", and proves it by rendering
before and after. The platform calls it through
`devdojo/app/Services/Sites/Engine/SyntaxConverter.php` (JSON on stdin, one
JSON answer). Export follows the same shape.

**The app already exports rendered HTML.** `app.devdojo/packages/export`
(4,200 lines of TypeScript) prints a frame's rendered HTML as HTML, React, Vue,
Svelte, Blade or Alpine, and wraps the page's scripts in a mount hook. It
knows `class` → `className`, style strings → objects, void tags and so on. It
has no props, no loops and no conditions, because rendered HTML has none. It
stays as it is, for Designer frames and designs, which have no block source.

**Behaviour today lives outside the block.** Counted across the 79 catalog
templates on 2026-11-20:

- 1,501 block components; 123 (8%) have any behaviour hook or script.
- No view uses Alpine (`x-data` appears in 0 of 2,167 view files).
- Behaviour is plain JavaScript reading `data-*` hooks (`data-mobile-toggle`,
  `data-dropdown`, `data-pricing-toggle`), written in each template's
  `public/js/main.js`, which reaches across the whole document
  (`document.getElementById('header')`).
- 7 sections carry their own `<script>` and hand-roll a run-once guard
  (`:not([data-pricing-04-ready])`).
- What the 123 do: on/off (mobile menu 59, dropdown 26, FAQ 15), pick one
  (pricing period 26, filters 6, tabs 3). 33 blocks have form inputs, nearly
  all newsletter or contact forms that submit.
- `data-reveal` (scroll-in animation) appears 2,868 times and is site-wide.
  Its CSS is gated on a `.js` class the site script adds, so a block without
  the script stays visible.

**The section library is a different picture.** The 369 sections under
`devdojo/resources/views/sites/sections` (what **Add a Section** inserts) are
static `.blade.php` files with no `@props`, pasted into a page as markup.
40 of them use Alpine (`x-data`), and 24 carry a `<script>`. The copy-paste
component library (`devdojo/resources/sites-components`) has 36 files with a
per-copy inline `<script>`. The catalog's views are all `.blade.php` today;
the Studio syntax is what new sites made in the app are written in.

**A section is not always a component.** In the app a section is either a
component placed with a tag (`type: 'include'`), or markup written straight
into the page (`type: 'html'`, which is what a library insert into a page
produces). It can sit in the page, in the layout's header or footer, or
nested in another section (`app.devdojo/packages/sites/src/types.ts`,
`SiteSection`).

**Capital `<Script>` is free.** `htmlscan` treats `Script` in any case as a raw
`<script>` today, the docs list `Script` and `Style` as reserved names, and no
catalog template or library component writes the capital spelling.

**The engine's scanner has three mirrors**, each held to it by tests, and
each reads `Script` as raw text today: `devdojo`'s
`app/Services/Sites/Sections/StudioScanner.php` and
`resources/js/sites/section-source.js`, and the app's
`packages/editor/src/studio/code/markup.ts`. A change to what `<Script>`
means is made in all four.

## 3. Goals, non-goals, anti-goals

**Goals**

1. Export one block, with the components it uses, as React, Vue, Svelte,
   Alpine or Blade/Laravel.
2. Export one page (its layout, its sections, the values it passes them) as
   the same five.
3. Output reads as hand-written: named props with defaults, types from the
   `.yml`, loops and conditions in the framework's own syntax, formatted.
4. Output is provably right: it renders the same page the engine renders.
5. A block can own its behaviour in a `<Script>` tag, which runs once per page
   on a DevDojo site and travels with the block into every export.
6. Works for a block written in either syntax (`.html` or `.blade.php`).

**Non-goals (v1)**

- Rewriting a block's JavaScript into framework state (`useState`, `ref`,
  `$state`). v1 wraps the same script in the framework's mount hook. Native
  state needs a declared-state layer; that is its own later spec (§12).
- Two-way data binding. Same later spec.
- Livewire components. The Laravel target is a Blade component.
- A whole site as a runnable Next, Nuxt or SvelteKit project (routing, a
  build, data fetching). A page export is components and a page file.
- Importing React, Vue or Svelte back into DevDojo. Export is one way.
- Minifying inside the engine (§7.5).
- Forms backends. A form drawn by DevDojo (`<form data-design>`) or wired to
  DevDojo at serve time exports as its markup with a note.
- Changing `app.devdojo/packages/export`.

**Anti-goals: these make it a failure even if it "works"**

- Code that renders something different from the DevDojo page and says
  nothing. Every difference is either refused or listed in `notes`.
- Code that reads as machine output: `dangerouslySetInnerHTML` around a whole
  block, helper soup, one giant component, props named `prop1`.
- A new parser for the language, anywhere. Export reads the tree this repo
  already builds. (The three scanner mirrors that exist learn `<Script>`;
  nothing new is added beside them.)
- Any change to what an existing site renders. `<Script>` is new meaning for
  a spelling nobody uses; everything else in the engine is additive.

## 4. Approaches

Three ways to build the export, and which one this design takes.

**A. Printers in the engine (Go). Chosen.** DevDojo Framework parses the block,
reads its HTML, and prints each target. One binary serves every door: the
platform's token API, MCP, the `@devdojo/sites` CLI, and the WASM build
(`cmd/devdojo-wasm`), so the app could even export with no round trip
later. One implementation of what `<If>` means. Cost: the HTML-attribute
knowledge that `packages/export` already has in TypeScript (`className`,
style objects, void tags, boolean attributes) is written again in Go.
To keep the two from drifting, the mapping tables are held to one shared
fixture file, the way `compose-cases.json` holds the design composer's port.

**B. Engine emits a neutral tree as JSON, TypeScript prints it.** Reuses
`packages/export`'s printers. But the platform (PHP) and MCP could not print,
the logic splits across two repos, and the printers would still have to learn
conditions, loops, props and expressions, which is the hard half. Rejected.

**C. All in TypeScript in the app.** Needs a second parser for the Studio
syntax and for Blade. The two would drift. Rejected (an anti-goal).

**Tony's decisions:** build it in DevDojo Framework; the tag is capital
`<Script>` (not `<script devdojo>`), with a lint for the lowercase slip; v1
wraps scripts and does not rewrite them; state and binding come later; the
five targets are React, Vue, Svelte, Alpine and Blade/Laravel; the Alpine
export is finished HTML with Alpine behaviour (§5.7); Export is for every
account, free and paid; and the library sections that use Alpine today are
rewritten onto `<Script>` rather than translated (§11 phase 5, appendix A).

## 5. The export contract

### 5.1 What is exported

| Ask | Answer |
|---|---|
| A component (`components/sections/hero.html`) | That component, every component it uses (recursively), the theme stylesheet, and a usage snippet |
| A section on a page (component + the page's tag) | The same, and the usage snippet carries the values that page passes |
| A section written straight into a page or a layout (`type: 'html'`) | That stretch of the file as a component **with no props**, named from the section's label, plus whatever components it uses. A note says why it has no props. Loops and conditions written there still export as loops and conditions |
| A page (`pages/pricing.html`) | A page component, its layout as a component, every section component, the theme stylesheet |

A section is addressed the same way wherever it sits (page, layout header or
footer, nested): the caller resolves it to either a component and its tag, or
a stretch of a file. The engine never needs the platform's section numbering.

### 5.2 Props

Each name in `<Props>` becomes a prop with its default. The `.yml` supplies
the type when there is one; otherwise the type is read from the default and
from how the block uses the value.

| `.yml` type | Exported type | Notes |
|---|---|---|
| `text`, `textarea`, `url`, `image`, `color` | `string` | |
| `richtext` | `string` (HTML) | printed raw, see §5.6 |
| `number`, `range` | `number` | a text default `"3"` becomes `3` |
| `toggle` | `boolean` | DevDojo stores `"1"` or empty; the export says `true` / `false`, in defaults and in usage |
| `select` | a union of the option keys | `'left' \| 'center'` |
| `repeater` | an array of an item type built from `sub_fields` | `nestable` adds an optional `children` of the same item type |
| no `.yml`, text default | `string` | |
| no `.yml`, `:count="3"` / `:on="true"` | `number` / `boolean` | |
| no `.yml`, bare `badge` | optional, type from use (`<Foreach badge>` → array, `{{ badge.x }}` → object, else `string`) | |

A dashed prop (`logo-text`) is `logoText`, as the engine already reads it.
The `.yml`'s `label` and `description` become the prop's doc comment.

### 5.3 `site` and collections become props

A component may read `site` and any collection without being handed them.
The exporter finds every such free name and makes it a prop of the exported
component, whose **default is the value the site has today**:

- `{{ site.name }}` → a prop `site` whose default holds the members the block
  reads (`{ name: "Rowan Emerson" }`), not the whole of `site.json`.
- `<Foreach posts="post">` → a prop `posts` whose default is today's entries,
  cut to the fields the block reads when that can be known from the source,
  and whole otherwise (the list is passed to another component, or looped as
  an object).
- A default over 2 KB is written to a sibling data file (`Hero.data.ts`) and
  imported, so the component stays readable.

A repeater field with `source: site.nav_links` exports as its own prop
(`links`) with the resolved rows as the default.

### 5.4 Constructs

| Source | React | Vue | Svelte 5 |
|---|---|---|---|
| `{{ x }}` in text | `{x}` | `{{ x }}` | `{x}` |
| `href="{{ link.url }}"` | `href={link.url}` | `:href="link.url"` | `href={link.url}` |
| `class="btn btn-{{ variant }}"` | ``className={`btn btn-${variant}`}`` | ``:class="`btn btn-${variant}`"`` | `class="btn btn-{variant}"` |
| `<If a>…<ElseIf b>…<Else>…</If>` | `&&` for a lone `If`, a ternary chain otherwise | `v-if` / `v-else-if` / `v-else` (on the element, or a `<template>` when the branch is more than one node) | `{#if}{:else if}{:else}` |
| `<Foreach items="item">` | `items.map((item, i) => …)` with `key={i}` | `v-for="(item, i) in items" :key="i"` | `{#each items as item, i}` |
| `limit="3"` | `.slice(0, 3)` first | the same, in the `v-for` | the same, in the `#each` |
| `loop.index` / `iteration` / `first` / `last` / `count` / `even` / `odd` | `i`, `i + 1`, `i === 0`, `i === items.length - 1`, `items.length`, … | same | same |
| a component tag | an import and a tag | an import and a tag | an import and a tag |
| `<Slot />` in a component file | `{children}` | `<slot />` | `{@render children?.()}` |
| `<Slot name="head" />` | a `head` prop (`ReactNode`) | `<slot name="head" />` | `{@render head?.()}` |
| `{{ class([...]) }}` | a class list filtered and joined, no library | `:class` with an array/object | `class={[…]}` |
| `{{ attributes }}` / `attributes.merge([...])` | `{...rest}`, with `className` merged | `v-bind="$attrs"` (`inheritAttrs: false` when not on the root) | `{...rest}` |
| `{!! x !!}` | §5.6 | `v-html` | `{@html x}` |
| `<Vite>` | dropped, with a note (the theme file replaces it) | same | same |

Expressions: `post.title`, `a ?? 'b'`, `!x`, `&&`, `||` and comparisons print
as themselves. Text joined with ` . ` prints as a template literal.

**Component names.** The exported name is the file's path in PascalCase
(`sections/hero.html` → `SectionsHero`), which is what the source already
calls it. Files are flat: `SectionsHero.tsx`, `SectionsHero.vue`,
`SectionsHero.svelte`. Two files can print the same name
(`sections/hero.html` and `sections-hero.html`); when both are in one export
the second is `SectionsHero2` and a note says so.

### 5.5 Where PHP and JavaScript disagree

The engine evaluates with PHP's rules. The exporter must not let that
difference leak silently.

- **An empty list is false in PHP and true in JavaScript.** When a condition
  tests a value the exporter knows is a list (a repeater, a collection, a
  value used in a `<Foreach>`), it prints `items.length > 0`.
- **Equality.** `plan == 'pro'` prints `===` when both sides are known to be
  the same type (the overwhelmingly common case), `==` otherwise.
- **A value whose type cannot be known** and is tested for truth: print plain
  JavaScript truthiness and add a note naming the prop.
- **The text `"0"`** is false in PHP and true in JavaScript. Accepted and
  documented; no block in the catalog depends on it.
- **Looping an object** (`<Foreach site.social="key, value">`) prints
  `Object.entries(…)`.

### 5.6 What is refused, and what is noted

Export follows convert's rule: right, or refused with a `file:line:col`
message. Nothing is guessed.

**Refused:**

- `<Break>` anywhere but the leading "limit" form, and `<Continue>` in a loop
  whose body reads `loop.*` (a filter would renumber the loop). A leading
  `<Continue condition>` in a loop that does not read `loop.*` becomes a
  `.filter()`.
- Anything the engine reports as `app-tier` in a Blade file (`@auth`, `@php`,
  a function call), for every target except Blade/Laravel, where it is
  carried through as written.
- A component the exporter cannot find.
- For React, Vue and Svelte: a block that carries Alpine (`x-data`,
  `x-show`, `@click`, `:class` and the rest). Alpine and a framework would
  fight over the same elements, and Vue would read `:class` and `@click` as
  its own. The sentence says the behaviour is written in Alpine and names
  the two targets that take it as it is: Alpine and Blade/Laravel. The
  library's own sections stop needing this once they are rewritten
  (appendix A); the rule stays for copies already pasted into customer
  sites and for Alpine a person wrote themselves.

**Exported, with a line in `notes`:**

- `{!! x !!}` in React or Vue when it is not the only child of its element:
  it is wrapped in a `<span>` that carries the raw HTML. (When it is the only
  child, the parent carries it and nothing is added.)
- A lowercase `<script>` or a `<style>` in the block: carried as written.
- A form DevDojo draws or wires.
- A hook attribute the site's shared script acts on (`data-reveal`): the
  markup is kept, the note says what animated it.
- An unknown-type truth test (§5.5).

### 5.7 The five targets

| Target | Files | Shape |
|---|---|---|
| **React** | `.tsx` (or `.jsx` by option) | Function component, a `Props` type, defaults in the destructuring, React 18 and up. No runtime dependency beyond React. |
| **Vue** | `.vue` | `<script setup lang="ts">`, `defineProps` with `withDefaults`, Vue 3. |
| **Svelte** | `.svelte` | Svelte 5: `$props()` with defaults, snippets for slots. |
| **Alpine** | `.html` | See below. |
| **Blade/Laravel** | `.blade.php` | An anonymous component with `@props`, for a real Laravel app. See below. |

**Alpine** has no props and no components, and looping in the browser
(`x-for`) would make a marketing block render late and hide it from crawlers.
So the Alpine target is the block **rendered** with the values in play (the
page's values for a section, the defaults for a component), nested components
included, as one HTML file, with its behaviour attached the Alpine way (§7.4).
A block with no behaviour exports as plain HTML and the note says so. Native
Alpine state (`x-show`, `@click`) arrives with the declared-state spec.

**Blade/Laravel** is the Studio-to-Blade converter's output with two changes
so it stands alone in an app that has no `resources/data`:

- `site` and collection reads become `@props` with defaults, as in §5.3.
- Defaults are written as objects (`(object) [...]`), so the body keeps
  `$link->url` exactly as the converter prints it, and an Eloquent model can
  be passed in its place.

The file is real Blade and is checked by the existing Laravel oracle.

### 5.8 The theme stylesheet

Blocks style themselves with the site's tokens (`bg-background`, `text-ink`).
Without the site's stylesheet an exported block looks broken. Every export
therefore carries one file, `devdojo-theme.css`: the stylesheets the site's
layout names in its `<Vite>` tag, joined in that order. (Most sites have one,
`resources/css/site.css`; 12 catalog templates keep their tokens in a
separate `resources/css/theme.css` loaded first.) A leading
`@import "tailwindcss"` is left out, since the project brings its own
(13 catalog `site.css` files start with one). The note: Tailwind v4, import
this once after Tailwind. Cutting the file down to what one block uses is a
later improvement, not v1.

### 5.9 Formatting

The printers format their own output (two spaces, one attribute per line past
100 columns, a blank line between imports, types and the component). Output
is stable: the same input prints the same bytes. No dependency on Prettier.

Whitespace that the browser shows is kept. Text between inline elements must
survive pretty-printing (`{' '}` in JSX where a line break would eat a space).

## 6. The engine surface

### 6.1 Command and protocol

```bash
devdojo export            # JSON on stdin, JSON on stdout, like convert
devdojo export my-site --component sections/hero --to react   # for people
```

```jsonc
// stdin
{
  "to": "react",                          // react | vue | svelte | alpine | blade
  "files": { "resources/views/…": "…", "resources/data/site.json": "…", "resources/css/site.css": "…" },
  "component": "sections/hero",           // or:
  "page": "resources/views/pages/pricing.html",
  "attributes": "heading=\"Ship faster\" :items=\"features\"",   // with "component": the tag's attributes as written on the page, for the usage snippet
  "fragment": { "file": "resources/views/pages/index.html", "start": 412, "end": 2890, "name": "Hero" },   // or: a stretch of a file (byte offsets), for a section written straight into it
  "schemas": { "resources/views/components/sections/hero.yml": { "title": "…", "fields": { … } } },
  "data": { },                            // optional, as compile takes it
  "options": { "typescript": true }
}

// stdout
{
  "engine": "1.1.0",
  "files":  [ { "path": "SectionsHero.tsx", "contents": "…", "language": "tsx" }, { "path": "devdojo-theme.css", … } ],
  "entry":  "SectionsHero.tsx",
  "usage":  "<SectionsHero heading=\"Ship faster\" />",
  "notes":  [ "…plain sentences for the person…" ],
  "diagnostics": [ { "file": "…", "line": 12, "col": 3, "kind": "error", "message": "…" } ]
}
```

A diagnostic of kind `error` means no `files`. `schemas` is the `.yml`
already parsed to JSON by the caller, because the engine's core reads no
YAML. Without a schema the exporter falls back to reading types from
`<Props>` and use (§5.2). The directory form of the CLI works without
schemas in v1.

### 6.2 New packages

- `blade/markup`: reads the tree's `Text`, `Echo` and control nodes into an
  element tree (elements, attributes whose values are a mix of text and
  expressions, children that may be elements, text, echoes, conditions,
  loops, components). Byte-faithful to what it cannot understand: a
  construct that will not sit in a tree (a tag opened in one branch of an
  `<If>` and closed in another) is a refusal, not a guess.
- `blade/export`: one printer per target over that tree, the prop and type
  resolver, the free-name analysis for `site` and collections, and the
  `notes`.
- `blade/expr` gains an exported way to walk a parsed expression (its nodes
  are private today).

Export ships as engine **1.1.0**; `<Script>` (§7) ships later as **1.2.0**,
with its own gate, because it changes what a file means and export does not.
`devdojo`'s `EngineVersion` gains a check for each, the way it has one for
"reads the Studio syntax".

## 7. `<Script>`: a block owns its behaviour

### 7.1 The rule for authors

A component file may hold one `<Script>` tag, and it is the **last thing in
the file**: props first, then the markup, then the script that works on it.
Its body is the setup for **one copy** of the block and is given two names:

- `root`: the block's outermost element.
- `signal`: an `AbortSignal` that fires when this copy goes away.

```html
<Props heading="Pricing" />

<section>
  <button data-more-toggle aria-expanded="false">Show all plans</button>
  …
</section>

<Script>
  const button = root.querySelector('[data-more-toggle]')
  button.addEventListener('click', () => {
    const open = root.toggleAttribute('data-expanded')
    button.setAttribute('aria-expanded', String(open))
  }, { signal })
</Script>
```

**Why the bottom, and what it asks of the engine.** Tony's call: a reader
should meet the props and the markup before the code that acts on them, and
it is where the catalog's hand-written section scripts already sit. The
catch is a layout: it prints the page's collected scripts at one place near
`</body>`, and its own `<Script>` comes after that point in the source. So
**neither tier may treat the script as output at the place it is written**.
Both lift it out when the file is read (the engine at parse time, the
`laravel/` package in its precompiler) and hand it over before any of the
file's markup renders. Both syntaxes write it in the same place, so the
token streams still match and convert's proof is untouched. One fixture
must be a layout with a script of its own.

The standard, which a lint enforces (§7.6):

1. Look only inside `root`. Never `document.querySelector`,
   `document.getElementById` or `window.something = …`.
2. Every listener, observer and timer is tied to `signal`
   (`{ signal }`, or `signal.addEventListener('abort', …)`).
3. State lives in the DOM (`data-*` and `aria-*` attributes), and CSS reads
   it. That is how the templates already work.
4. Values the script needs from props go through the markup
   (`data-period="{{ period }}"`, read as `root.dataset.period`).
5. No `import`, no `export`, no top-level `await`.
6. The component has exactly one root element, and it is a plain HTML
   element (not a component tag). Otherwise there is nothing to call `root`.

A lowercase `<script>` keeps meaning exactly what it means today: left alone.

**Why no `props` in the script (a change from the first sketch).** Handing
props to the script on a DevDojo site means writing them into the page as
JSON, and Go and PHP would have to print that JSON byte for byte the same.
Rule 4 avoids the problem entirely, keeps the script identical in every
target, and matches how the catalog's scripts already read their values.

### 7.2 What the engine does with it

On a DevDojo site, for each component with a `<Script>` used on a page:

- The script is emitted **once per page**, however many copies of the block
  there are, wrapped so the body becomes a function of `(root, signal)`
  registered under the component's name.
- Each copy's root element is marked so the runtime can find it.
- A small runtime (written once, in this repo, embedded by both tiers) runs
  each registered function for each copy when the page is ready and again
  for new copies after `instant:navigated`, and aborts a copy's `signal`
  when the copy leaves the document.
- The scripts land at one place in the layout, at the end of `<body>`.

- Each emitted block script carries `data-instant-shared`, the mark instant
  navigation already adopts (§7.3).

**The Blade twin is a short form too.** The catalog's views are all Blade, so
a template author must never hand-write the wrapper or the marker: that
would freeze both in 79 repositories. A Blade file writes

```blade
@props(['heading' => 'Pricing'])

<section>…</section>

@blockscript
  const button = root.querySelector('[data-more-toggle]')
  …
@endblockscript
```

and the `laravel/` package teaches real Laravel the directive (a Blade
precompiler, which knows the file being compiled and so the component's
name), so both tiers print the same bytes and the oracle checks it like
every other construct. It must not be named `@script`: Livewire owns that
directive in a real Laravel app.

An **export** to Blade/Laravel (§5.7) is different: that app does not have
the package, so the exported file spells the script out in plain Laravel
(`@pushOnce('scripts', …)`, and the note says the layout needs
`@stack('scripts')`).

Left to the builder, within those constraints: the marker attribute; the
wrapper; how the runtime reaches the page in each tier (the same bytes in
both, once); how the layout's place for scripts is spelled in each syntax,
or whether the tiers agree on an automatic one before `</body>` (no
component in the catalog or the library is named `script` or `scripts`, so
a new reserved tag is free); and the diagnostic for a layout with no place
for them.

### 7.3 Instant navigation

`devdojo/resources/js/sites/instant-navigation.js` swaps `<main>` in place.
Its signature of the page's chrome skips `<script>` elements, so a page
that brings a block script the last page lacked still swaps in place. And
its `adoptScripts` takes on, from the new page, only external scripts and
inline ones marked `data-instant-shared`. So an unmarked block script would
**silently never run** for a block type first met after a swap. There is no
safe fallback here: the engine must emit the mark (§7.2), and check 5 of §10
must navigate to a page with a block type the first page did not have.
No change to the navigation runtime itself is expected.

### 7.4 In an export

The body is the same text in every target. Only the wrapper changes:

| Target | Wrapper |
|---|---|
| React | `useRef` on the root, `useEffect(() => { const root = ref.current; const controller = new AbortController(); const { signal } = controller; …body…; return () => controller.abort() }, [])` |
| Vue | a template ref, the body in `onMounted`, `controller.abort()` in `onBeforeUnmount` |
| Svelte | `bind:this` on the root, the body in `onMount`, which returns the abort |
| Alpine | `x-data` naming an `Alpine.data(…)` object whose `init()` runs the body with `this.$el` as `root` and whose `destroy()` aborts |
| Blade/Laravel | plain Laravel: `@pushOnce('scripts', …)` around the wrapped body and the runtime (§7.2) |

Shared, site-wide behaviour (scroll-in reveals, the header's scrolled state,
the scroll lock on `<html>`) belongs in the **layout's** `<Script>`, whose
root is the layout's own root element. A page export carries the layout and
so carries it. A lone block does not, and gets a note (§5.6).

### 7.5 Minifying

Not in the engine. Real Laravel does not minify, so the two tiers would stop
agreeing byte for byte, which is the rule this engine is built on, and the
engine has no minifier today. If it is wanted, it is a step at publish
(`devdojo`'s `Exporter`), over the block scripts only, and it can be added
without touching this design.

### 7.6 Lints

- A lowercase `<script>` in a component file that uses `root` or `signal` as
  a free name: "did you mean `<Script>`?"
- A `<Script>` body that breaks rules 1, 2 or 5 of §7.1 (a scan for
  `document.querySelector`, `document.getElementById`, `import`, `export`,
  and `addEventListener` with no `signal`). Warnings, not errors: the script
  still runs on a DevDojo site, but the export will note it.
- More than one `<Script>` in a file, a `<Script>` that is not the last
  thing in it, or no single plain root: an error.

### 7.7 Rolling it out safely

`<Script>` gains meaning in engine 1.2.0. Before the tag reaches production,
run the engine's corpus check (`DEVDOJO_CORPUS`) over real customer sites
for any existing capital `<Script>`, since a customer may have written one
where the catalog did not.

## 8. The platform (`devdojo/`)

- `App\Services\Sites\Engine\FrameworkExporter`, beside `SyntaxConverter`:
  gathers the site's source, parses every `.yml` it needs through
  `SectionSchema`, runs `devdojo export`, and returns the answer as a
  value. Whole or not at all.
- Token API for the app, read scope, the draft as it stands (no lock, no
  revision, nothing written):

  `POST api/sites/{slug}/export`
  `{ to, component? | page?, section?, typescript? }` →
  `{ files, entry, usage, notes }`, or 422 `export-refused` with the
  diagnostics, or 503 `engine-unavailable` on an engine before 1.1.0.

- Its own rate limit. No credits: nothing here calls a model. No plan
  check: every account can export. The refusal path goes through one place
  so a plan check could be added later without reshaping the endpoint.
- Docs ride along (the repo's rule): a public page under `docs/sites/public`
  ("Export to React, Vue, Svelte, Alpine and Laravel"), the `<Script>` rules
  added to `20-components.md` and `22-syntax-reference.md`, and a private
  page for the seam.
- The caller resolves a section before asking the engine: a component
  section becomes `component` + `attributes`, a section written straight
  into a file becomes `fragment` with its byte offsets (the section editor's
  `TagSyntax` already knows both).
- The AI's prompts learn `<Script>` through the existing single source
  (`BladeSitePrompt` + `SyntaxWords`, and both dialect skills), so a
  generated block puts its behaviour in the block.
- With `<Script>` (phase 3): `StudioScanner.php` and
  `resources/js/sites/section-source.js` learn the tag, in step with the
  engine. A script is never a section and never numbered by `NodeMarker`.
- **A library section with behaviour is inserted as a component.**
  `<Script>` is legal only in a component file, and a library insert into a
  page is pasted markup. So a library section that owns a script goes in the
  way a layout insert already does (`Sections\LayoutSections`): written to
  `components/sections/{name}` and mounted by its tag. A section with no
  script is pasted as it is today.

## 9. The app (`app.devdojo/`)

- **Section**: one new action, **Export…**, added once in
  `packages/editor/src/studio/sectionActions.ts`, which feeds both the ⋯
  menu on a section and its right-click menu. It sits with Duplicate.
- **Page**: **Export Page…** in the page frame's ⋯ menu
  (`packages/editor/src/canvas/CanvasMenu.tsx`), beside Edit Code.
- **The dialog**: the existing `ExportDialog` pattern. A row of five targets
  (React, Vue, Svelte, Alpine, Laravel), a TypeScript switch for React, a file
  list, the code with **Copy**, **Download .zip** for everything, the usage
  snippet, and the notes in plain sentences under the code. A refusal shows
  the engine's sentence and the line.
- The client call lives in `packages/sites` beside `sections.ts`.
- Export is offered for every section, whatever its `type` and `scope`.
- With `<Script>` (phase 3): the code drawer's scanner
  (`packages/editor/src/studio/code/markup.ts`) learns the tag, and the
  editor colours its body as JavaScript.
- Nothing a person reads in the app says "Studio".

## 10. Proof

"Really good code" is checked, not hoped for. A new `export-oracle/` (Node)
sits beside the Laravel `oracle/`.

1. **Same render.** For every export fixture and target: export it, render
   it on the server (`react-dom/server`, `@vue/server-renderer`,
   `svelte/server`; Alpine in a headless browser after Alpine starts; Blade
   through the Laravel oracle), once with no props and once with a set of
   passed values. Parse both that HTML and the engine's own render into a DOM,
   normalise what cannot matter (attribute order, entity spelling, the
   whitespace HTML collapses, React's text-separator comments, the documented
   raw-HTML wrapper), and require them equal.
2. **It compiles.** `tsc --strict` on the React output, `vue-tsc` on Vue,
   `svelte-check` on Svelte. Zero errors.
3. **Golden files.** The printed code of each fixture is committed, so a
   change to formatting is seen in review.
4. **The corpus.** Every section in the platform's section library and every
   block of every catalog template is exported to every target. Each one
   either passes check 1 or is refused with a named reason. Zero that export
   and render differently. The pass rate is printed and tracked.
5. **Behaviour** (with `<Script>`): for fixtures with a script, a headless
   browser clicks the same hooks in the DevDojo render and in each export
   and compares the attributes afterwards.
6. **Nothing hidden.** An exported block with `devdojo-theme.css` and no site script
   shows every element the DevDojo page shows.
7. **Fuzzing.** On arbitrary input the exporter may refuse; it never panics,
   and what it prints must parse.

## 11. Order of work

Each phase ships on its own.

1. **Engine: plain blocks.** `blade/markup`, `blade/export`, the protocol,
   the CLI, React first, then Vue, Svelte, Blade and Alpine. `export-oracle/`
   and checks 1 to 4. Roughly 92% of catalog blocks need nothing more.
2. **Platform and app.** `FrameworkExporter`, the endpoint, the two menu
   items, the dialog, the theme file, docs.
3. **`<Script>`** (engine 1.2.0). The tag and `@blockscript`, the `laravel/`
   package's half, the runtime, the `data-instant-shared` mark, the lints,
   the three scanner mirrors, the wrappers in each export, checks 5 and 6,
   the prompts, and library sections with a script inserted as components.
4. **Page export.** The page component and its layout. A layout is a whole
   document (doctype, `<html>`, `<head>`), which a Vue or Svelte component
   cannot be. So the layout exports as a component of what is inside
   `<body>`, around a slot, on a wrapper element that takes the classes and
   data attributes of `<html>` and `<body>` (what `packages/export` already
   does for a frame). That wrapper is `root` for the layout's `<Script>`.
   The `<head>`'s own parts (title, meta, font links) go to a plain
   `head.html` with a note, since every framework has its own way to set
   them. Check 1 compares the body. Dynamic pages (`[post.slug]`) export one
   component taking `post`, with a note that routing is the project's own.
5. **Move behaviour into blocks.** This is content work, and the largest
   share of the hours. In this order:
   - The 40 library sections that use Alpine (appendix A) come off it. What
     they use is small: 54 `{ hover: false }` (which becomes plain CSS,
     `hover:` and `group-hover:`, with no script at all), on/off toggles for
     FAQs and mobile menus, three pricing switches, one tab set and one
     pager (each a short `<Script>`). Each is checked in a browser against
     the old one.
   - The 24 library sections with a plain `<script>` move to `<Script>`.
   - Each catalog template's `public/js/main.js`, one template at a time:
     what belongs to one block moves into that block, what is site-wide
     moves into the layout's script.

   A section already pasted into a customer's site keeps the copy it has;
   only new inserts get the rewritten one.

## 12. Later, in its own spec

**Declared state and binding.** To print `useState`, `ref`, `$state` and
native Alpine instead of a wrapped script, the compiler has to be able to
read the state, so it must be *declared*, not hidden in free-form
JavaScript: "this block has `open`, it starts false, this button flips it,
this panel shows while it is true." The catalog needs two patterns almost
everywhere (on/off, pick one of several); a value kept in step with an input
is rare. Tony's direction is DevDojo's own small layer on top of `<Script>`,
not Alpine. Livewire export would follow from the same work.

Also later: `props` inside a `<Script>`; cutting `devdojo-theme.css` down to one
block; export from MCP and the `@devdojo/sites` CLI; exporting in the browser
through the WASM build.

## 13. Left to the builder

- The internal shape of the element tree and how it stays byte-faithful.
- How types are inferred when there is no `.yml`, beyond the table in §5.2.
- Formatting details past §5.9.
- The marker attribute, the runtime's code and how it is delivered (§7.2).
- The normaliser's exact rules (§10.1), provided each one is written down.
- File names for data files and the zip's layout.
- Which fixtures to write, provided every row of §5.4 and §5.6 has one.

## 14. Calls made while writing this, for Tony to check

None of these was asked; each is a default with its reason, and any of them
can be turned over before the plan is written.

1. **`<Script>` is the last thing in the file, as a rule and not only a
   recommendation** (§7.1). The bottom is Tony's decision; making it the one
   allowed place is the author's call, so four scanners and the AI have a
   single spot to look.
2. **A `<Script>` is not handed `props`.** Values come through `data-*`
   attributes (§7.1). It keeps the script identical in every target.
3. **Blade's spelling is `@blockscript` … `@endblockscript`** (§7.2). The
   name is open; it only must not be `@script`.
4. **A library section that owns a script is inserted as a component file**,
   not pasted into the page (§8).
5. **A section written straight into a page exports with no props** (§5.1).
   Turning its words into props automatically is a later idea.
6. **A page's layout exports as what is inside `<body>`**, with the
   `<head>`'s parts in a plain `head.html` (§11 phase 4).
7. **Minifying happens at publish, if at all**, not in the engine (§7.5).
8. **Svelte 5** (runes), not Svelte 4. **React** is `.tsx` by default.
9. **Export is engine 1.1.0 and `<Script>` is 1.2.0**, each with its own
   gate (§6.2).
10. **A block that carries Alpine is refused for React, Vue and Svelte**
    until it is rewritten (§5.6).

## Appendix A: the 40 library sections that use Alpine today

Under `devdojo/resources/views/sites/sections/`, each a `.blade.php`:

- **headers** (19): `header-01`, `header-02`, `header-03`, `header-04`, `header-06`, `header-07`, `header-08`, `header-09`, `header-10`, `header-12`, `header-15`, `header-16`, `header-20`, `header-23`, `header-24`, `header-25`, `header-30`, `header-38`, `header-39`
- **navigations** (8): `navigation-01`, `navigation-08`, `navigation-09`, `navigation-10`, `navigation-13`, `navigation-14`, `navigation-15`, `navigation-18`
- **faqs** (7): `faqs-04`, `faqs-05`, `faqs-07`, `faqs-08`, `faqs-09`, `faqs-10`, `faqs-11`
- **pricing** (3): `pricing-04`, `pricing-05`, `pricing-09`
- **coming-soon** (1): `coming-soon-02`
- **content** (1): `content-15`
- **features** (1): `features-10`
