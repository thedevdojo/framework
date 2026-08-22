# Pocketknife

<img src="https://cdn.devdojo.com/images/august2026/pocketknife-banner.jpg" style="height:auto; width:100%">

**The portable Blade.** A Go engine that renders Laravel Blade templates — no PHP, no `eval`, no runtime. Write real Blade, get static HTML, and keep the option to boot the exact same files in a real Laravel app whenever you outgrow static.

```blade
<h1>{{ $site->name }}</h1>

@foreach ($plans as $plan)
    <li>{{ $loop->iteration }}. {{ $plan->name }} — {{ $plan->price }}</li>
@endforeach
```

```html
<h1>Pocketknife</h1>

    <li>1. Starter — Free</li>
    <li>2. Pro — $12/mo</li>
```

## Why

Blade is a great templating language, but running it requires PHP executing arbitrary code. If you wanted to give your users the option to edit templates, there is not safe way to gaurantee that they will not run any malicious code, until now. Pocketknife renders a **subset of Blade** and the output is nearly identical to what a real Laravel app produces from the same files.

So a Pocketknife site is literally a slice of a Laravel app. Toggle it into a real app and zero files move.

## Quick taste

**Components work like real Blade components** — `@props` defaults, attributes, slots:

```blade
{{-- resources/views/components/sections/hero.blade.php --}}
@props(['heading' => 'Hello', 'cta' => 'Learn more'])
<section class="hero">
    <h1>{{ $heading }}</h1>
    <a href="/signup">{{ $cta }}</a>
</section>
```

```blade
{{-- resources/views/pages/index.blade.php --}}
<x-sections.hero heading="Build sites with Blade" cta="Get started"/>
```

```html
<section class="hero">
    <h1>Build sites with Blade</h1>
    <a href="/signup">Get started</a>
</section>
```

**Data comes from JSON and markdown files**, bound the way Laravel would bind them: `resources/data/site.json` → `$site`, `resources/data/collections/plans.json` → `$plans`, `resources/data/content/post/*.md` → `$post`. All available everywhere — pages, layouts, components.

**Anything outside the subset is not an error — it's a signal.** `@auth`, method calls, `@php`: all valid Blade, just constructs that need a running app. The engine reports them as structured `app-tier` diagnostics instead of crashing or silently dropping them:

```
index.blade.php:2:1 [app-tier] directive @auth is outside the Pocketknife subset; requires the PHP app tier
```

## The subset

Output `{{ }}` / `{!! !!}` / `{{-- --}}` · `@if` / `@elseif` / `@else` · `@foreach` with the full `$loop` object, `@break` / `@continue` · components with `@props`, bound attributes (`:items="$plans"`), `$attributes`, default and named slots · expressions with `->` / `[]` access, `??`, comparisons, booleans — all with PHP semantics (truthiness, loose equality, stringification) · `@vite` rendered as its static projection (Tailwind Play CDN + inlined CSS).

No function calls, no assignments, no `@php`. Undefined variables are compile-time errors with file/line/col — `??` is the sanctioned default (`{{ $tagline ?? 'Welcome' }}`).

## Usage

```bash
go build -o /usr/local/bin/pocketknife ./cmd/pocketknife
```

**Lint** a site directory — silence means everything is inside the static subset:

```bash
pocketknife lint example
```

**Compile** speaks one JSON document on stdin, one on stdout — files in, files + deps + diagnostics out. No disk, no eval:

```bash
echo '{"files": {"resources/views/pages/index.blade.php": "<h1>{{ $title }}</h1>", "resources/data/site.json": "{}"}, "data": {"title": "Hi"}}' \
  | pocketknife compile | jq -r '.files["index.html"]'
# <h1>Hi</h1>
```

Or embed the engine directly from Go:

```go
result := pocketknife.Compile(vfs.Dir("my-site"), []string{"resources/views/pages/index.blade.php"}, pocketknife.Options{})
```

The [`example/`](example/) directory is the smallest useful site — a page, a layout, a component fed by collection data — with a `render.sh` that runs the whole pipeline.

## Repo layout

| Path | What it is |
|---|---|
| `blade/` | The Go engine: scanner, parser, renderer, expression evaluator. Stdlib-only, never panics on user input. |
| `laravel/` | The `devdojo/pocketknife` Composer package that makes the same files render in a real Laravel app. |
| `fixtures/` | The contract — conformance cases rendered by both engines. |
| `oracle/` | A minimal Laravel app that runs the fixtures through genuine Blade in CI. |
| `cmd/pocketknife/` | The CLI: `compile`, `lint`, `version`. |

Full design specs live in [`docs/specs/`](docs/specs/).
