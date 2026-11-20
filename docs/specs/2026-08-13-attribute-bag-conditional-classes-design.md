# Attribute bag + conditional classes (engine 0.2.0)

> **A note on names:** this was written when the project was called Pocketknife. It is now DevDojo Framework (`github.com/thedevdojo/framework`, binary `devdojo`). The names below are left as they were written.

**Date:** 2026-08-13
**Status:** Shipped. Oracle-verified (all `oracle: true` fixtures byte-identical
against real Blade, Laravel 12, `make conformance`).
**Supersedes:** the v1 design's `$attributes` exclusions — §3.3 "Undeclared
attributes are dropped", the §"app-tier" listing of `$attributes`, and the v1
scope table row "Named slots / `$attributes`".

## What changed

Three Blade features move from app-tier into the modeled subset:

1. **`$attributes`** — every component scope now binds Laravel's
   `ComponentAttributeBag`, with the modeled method subset:
   `merge`, `class`, `style`, `only`, `except`, `has`, `hasAny`, `missing`,
   `get`, `first`, `whereStartsWith`, `whereDoesntStartWith`, `thatStartWith`,
   `isEmpty`, `isNotEmpty`, `all`/`toArray`/`getAttributes` (case-insensitive,
   like PHP). Echoing the bag — `{{ $attributes }}` or any method-chain result
   — emits the imploded ` key="value"` string RAW in both echo forms (the bag
   is Htmlable; `e()` returns `toHtml()` untouched).
2. **`@class([...])` / `@style([...])`** — compile to `class="…"` / `style="…"`
   via `Arr::toCssClasses` / `Arr::toCssStyles`: numeric-key entries always
   contribute their value; string-key entries contribute the key when the value
   is truthy; style rules gain exactly one trailing `;`. Directive newline-eat
   applies (they compile to `<?php echo …; ?>`).
3. **Blade comments** were already modeled (v1); this release only adds edge
   fixtures (`comments-edge`: earliest-`--}}` termination, quotes, directives
   inside comments).

Supporting grammar (blade/expr):

- **Array literals** `['a', 'k' => $v, …]` evaluate to a new ordered value
  type `*expr.Arr` carrying PHP's key model (auto-indexing, key casts:
  canonical-integer strings → int, float → trunc, bool → 0/1, null → "").
  Arrays remain non-stringable; they are truthy when non-empty, loosely
  compare key-wise, index like maps, and `@foreach` iterates them in insertion
  order.
- **Method calls** `->name(args…)` parse everywhere but evaluate ONLY on a
  bag receiver with a modeled method. Every other case — non-bag receiver, a
  receiver that fails to evaluate, closure-taking bag methods (`filter`,
  `map`, `when*`…), `prepends`, unknown (Macroable) names — reports the same
  app-tier `method call (->name())` classification the parser used to emit,
  so no template that previously classified app-tier downgrades to a hard
  error.

## Bag construction (the sanitize model)

Mirrors the compiled tag + `compileProps` pipeline, oracle-verified by the
`attribute-bag-escaping` fixture:

- The bag holds every attribute whose name is NOT claimed by `@props`. The
  claim set is `extractPropNames`: each declared name plus its `Str::kebab`
  form. Order is tag order; a repeated name overwrites in place.
- Values enter the bag through `sanitizeComponentAttribute`: a **bound**
  attribute whose evaluated value is a string is `e()`-escaped; static
  strings, bools, numbers, null pass through. A bound attribute whose
  expression fails to evaluate is diagnosed and left out of the bag.
- A **valueless** attribute (`<x-btn wide/>`) is bool `true` (new
  `scanner.Attr.Valueless`; distinct from `wide=""`). In the imploded HTML,
  `true` renders the attribute name as its value — except `x-data` and
  `wire:*`, which render `=""`.
- `::foo="…"` is the literal-colon escape: a STATIC attribute named `:foo`
  (Alpine/Vue bindings pass through the bag).
- An entry literally named `attributes` holding another bag folds in via
  `parent.merge(rest, escape=false)` — `setAttributes`' pass-through case.

**Variable binding correction** (supersedes v1 §3.3): ALL passed attributes
bind component variables, not just declared props —
`partitionDataAndAttributes` returns every attribute as data for a classless
component. The camelCase name gets the RAW value (the data array is compiled
with `escapeBound = false`); a snake_case attribute's literal name gets the
SANITIZED value (it reaches the scope through the bag's original keys). So
`<x-tag :logo_text="'<b>'"/>` gives `$logoText == '<b>'` but
`$logo_text == '&lt;b&gt;'`, and `{{ $logo_text }}` double-encodes — real
Blade behavior, byte-matched.

## Diagnostics contract changes

- The `app-tier/$attributes` carve-out is REMOVED. `$attributes` is a real
  binding in components; on a page it is a genuine `error/undefined-variable`,
  which is also what real Blade raises there.
- A malformed `@props` still diagnoses `props`, but passed attributes bind
  anyway (they are view data, independent of the `@props` line).
- `pocketknife lint` suppresses undeclared-attribute warnings for any
  component whose source mentions `$attributes` — extra attributes are the
  feature there, not typos.

## Deliberately out (app-tier, unchanged)

Closures in bag methods, `prepends()`, macros, `{{ $attributes }}` inside
static attribute values (`attribute-interpolation`), `@class` **inside a
component tag's attribute list** (`<x-btn @class(...)>` — the tag compiler
special-cases it in real Blade; the scanner leaves the tag unparsed), and
`merge` defaults with non-sequential integer keys (array_merge renumbering is
approximated by decimal-string names).
