# Pocketknife

<img src="https://cdn.devdojo.com/images/august2026/pocketknife-banner.jpg" style="height:auto; width:100%">

**The portable Blade.** A Go engine that renders Laravel Blade templates — no PHP, no `eval`, no runtime. Write real Blade, get static HTML. Need a full application, move the files into a Laravel app and you're good to go.

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

**Data comes from JSON and markdown files**, bound the way Laravel would bind them: `resources/data/site.json` → `$site`, `resources/data/collections/plans.json` → `$plans`, `resources/data/content/post/*.md` → `$post`.

A collection can also be a **folder of entry files** — `resources/data/collections/posts/<slug>.json`, one JSON object per entry, bound as `$posts` ordered by `date` descending then file name — for a blog too large to keep in one array file. The two forms bind identically; when a site has both for one name, the folder wins and the compile reports a collection error naming both paths.

## Two syntaxes

The same engine reads the same templates written as **HTML tags** instead of Blade directives — for people who know HTML and nothing else. A file's extension picks its syntax: `.blade.php` is Blade, `.html` is the HTML syntax.

```html
<!-- resources/views/pages/index.html -->
<Layout src="main.html" title="Blog">
  <Foreach posts="post" limit="3">
    <If post.featured>
      <PostCard :post="post" badge="Featured" />
    <Else>
      <PostCard :post="post" />
    </If>
  </Foreach>
  <Button href="/blog">All posts</Button>
</Layout>
```

| HTML syntax | Blade |
|---|---|
| `{{ post.title }}` | `{{ $post->title }}` |
| `<If condition="plan == 'pro'">` · `<If featured>` · `<If plan="pro">` | `@if (…)` |
| `<ElseIf condition="…">` · `<Else>` · `</If>` | `@elseif (…)` · `@else` · `@endif` |
| `<Foreach posts="post">` … `</Foreach>` | `@foreach ($posts as $post)` … `@endforeach` |
| `<Layout src="main.html">` | `<x-layouts.main>` |
| `<Include src="button.html" />` | `<x-button />` |
| `<Button>` · `<SectionsHero heading="Hi" />` | `<x-button>` · `<x-sections.hero heading="Hi" />` |
| `<Props title="Untitled" />` · `<Slot />` | `@props(['title' => 'Untitled'])` · `{{ $slot }}` |

A tag that starts with a capital letter belongs to the template; plain HTML is lowercase and is never touched. Every component gets its own tag, named after its file: `components/button.html` is `<Button>`, `components/sections/hero.html` is `<SectionsHero>`. In an `.html` file `@` is plain text, so Alpine's `@click` and CSS `@media` need no escaping.

Both syntaxes run through one parser and one renderer — the HTML scanner emits the very tokens the Blade scanner does — so they render the same bytes and cannot drift. [`example-html/`](example-html/) is [`example/`](example/) rewritten in the HTML syntax, and a test holds the two outputs identical. The full reference is [`docs/specs/2026-11-14-html-syntax-design.md`](docs/specs/2026-11-14-html-syntax-design.md).

**Convert either way.** A site written in one syntax can be rewritten in the other, in place:

```bash
pocketknife convert my-site --to html             # Blade → HTML syntax
pocketknife convert my-site --to blade            # HTML syntax → Blade
pocketknife convert my-site --to blade --dry-run  # say what would change, touch nothing
```

A conversion is exact or it does not happen: every converted file is read back and compared token for token, the site is rendered before and after and every page must come out byte for byte the same, and one problem anywhere leaves every file untouched. Blade that needs real Laravel (`@auth`, `route()`) is refused on its way to the HTML syntax. The Laravel package renders Blade only, so converting to Blade is also how an `.html` site moves into a real Laravel app.

## The subset

Output `{{ }}` / `{!! !!}` / `{{-- --}}` · `@if` / `@elseif` / `@else` · `@foreach` with the full `$loop` object, `@break` / `@continue` · components with `@props`, bound attributes (`:items="$plans"`), `$attributes`, default and named slots · `@class` / `@style` · expressions with `->` / `[]` access, `??`, comparisons, booleans, text joined with `.` — all with PHP semantics (truthiness, loose equality, stringification) · `@vite` rendered as its static projection (Tailwind Play CDN + inlined CSS).

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

A dynamic page — one template, one document per collection entry — renders in one request through `variants`: each variant names the entry, the data bound for that render alone (merged over `data`), and the output path, so the site's files are sent and parsed once rather than once per document:

```bash
echo '{"files": {"resources/views/pages/post/[post.slug].blade.php": "<h1>{{ $post->title }}</h1>"}, "variants": [{"entry": "resources/views/pages/post/[post.slug].blade.php", "data": {"post": {"title": "Hi"}}, "output": "post/hi/index.html"}]}' \
  | pocketknife compile | jq -r '.files["post/hi/index.html"]'
# <h1>Hi</h1>
```

Or embed the engine directly from Go:

```go
result := pocketknife.Compile(vfs.Dir("my-site"), []string{"resources/views/pages/index.blade.php"}, pocketknife.Options{})
```

The [`example/`](example/) directory is the smallest useful site — a page, a layout, a component fed by collection data — with a `render.sh` that runs the whole pipeline. [`example-html/`](example-html/) is the same site in the HTML syntax.

## Repo layout

| Path | What it is |
|---|---|
| `blade/` | The Go engine: scanner, parser, renderer, expression evaluator. Stdlib-only, never panics on user input. `blade/htmlscan` is the second scanner, for the HTML syntax; `blade/convert` rewrites a site from one syntax to the other. |
| `laravel/` | The `devdojo/pocketknife` Composer package that makes the same files render in a real Laravel app. |
| `fixtures/` | The contract — conformance cases rendered by both engines. |
| `oracle/` | A minimal Laravel app that runs the fixtures through genuine Blade in CI. |
| `cmd/pocketknife/` | The CLI: `compile`, `lint`, `convert`, `version`. |

Full design specs live in [`docs/specs/`](docs/specs/).
