# DevDojo Framework in detail

The [README](../README.md) gets you from nothing to a built site. This page is the rest: why the engine is shaped the way it is, both syntaxes side by side, the folders a site may use, the data it reads, and every command, including the JSON protocol a host program talks to.

DevDojo Framework reads two template syntaxes, and they are the same language written two ways:

| Syntax | Files | It looks like | Full guide |
|---|---|---|---|
| **Blade** | `.blade.php` | `@if`, `{{ $post->title }}`, `<x-sections.hero />` | [**The Blade syntax →**](blade-syntax.md) |
| **HTML** | `.html` | `<If>`, `{post.title}`, `<SectionsHero />` | [**The HTML syntax →**](html-syntax.md) |

The same page in each:

```blade
{{-- Blade — index.blade.php --}}
<h1>{{ $site->name }}</h1>

@foreach ($plans as $plan)
    <li>{{ $loop->iteration }}. {{ $plan->name }} — {{ $plan->price }}</li>
@endforeach
```

```html
{{-- The HTML syntax — index.html --}}
<h1>{site.name}</h1>

<Foreach plans="plan">
    <li>{loop.iteration}. {plan.name} — {plan.price}</li>
</Foreach>
```

Both compile to the same bytes:

```html
<h1>DevDojo Framework</h1>

    <li>1. Starter — Free</li>
    <li>2. Pro — $12/mo</li>
```

## Why

A template language that can run code cannot be handed to your users: there is no safe way to let them edit a template and guarantee nothing malicious runs. DevDojo Framework compiles a **closed language**. A template can read values, compare them, loop and use components — and nothing else. No function calls, no assignments, no PHP. What it cannot say, it cannot do.

Nothing is invented, either. The Blade syntax is a subset of real Laravel Blade, and a conformance suite renders every Blade case through both DevDojo Framework and genuine Laravel and compares the output byte for byte. So a DevDojo Framework site is literally a slice of a Laravel app: move the files into one and they keep working.

## The two syntaxes

A file's extension picks its syntax, one file at a time. Under each is one parser and one renderer — the HTML syntax's scanner emits the very tokens the Blade scanner does — so the two cannot drift apart.

**Components** work like real Blade components — props with defaults, attributes, slots:

```blade
{{-- src/components/sections/hero.blade.php --}}
@props(['heading' => 'Hello', 'cta' => 'Learn more'])
<section class="hero">
    <h1>{{ $heading }}</h1>
    <a href="/signup">{{ $cta }}</a>
</section>
```

```blade
{{-- src/pages/index.blade.php --}}
<x-sections.hero heading="Build sites, not servers" cta="Get started"/>
```

In the HTML syntax the same two files are:

```html
{{-- src/components/sections/hero.html --}}
<Props heading="Hello" cta="Learn more" />
<section class="hero">
    <h1>{heading}</h1>
    <a href="/signup">{cta}</a>
</section>
```

```html
{{-- src/pages/index.html --}}
<SectionsHero heading="Build sites, not servers" cta="Get started" />
```

Either way:

```html
<section class="hero">
    <h1>Build sites, not servers</h1>
    <a href="/signup">Get started</a>
</section>
```

Side by side:

| Blade | HTML |
|---|---|
| `{{ $post->title }}` | `{post.title}` |
| `@if ($plan == 'pro')` | `<If condition="plan == 'pro'">` · `<If plan="pro">` · `<If featured>` |
| `@elseif (…)` · `@else` · `@endif` | `<ElseIf condition="…">` · `<Else>` · `</If>` |
| `@foreach ($posts as $post)` … `@endforeach` | `<Foreach posts="post">` … `</Foreach>` |
| `<x-layouts.main>` | `<Layout src="main.html">` |
| `<x-button>` · `<x-sections.hero heading="Hi" />` | `<Button>` · `<SectionsHero heading="Hi" />` |
| `@props(['title' => 'Untitled'])` · `{{ $slot }}` | `<Props title="Untitled" />` · `<Slot />` |
| `<a @class(['btn', 'on' => $active])>` | `<a {class(['btn', 'on' => active])}>` |
| `{title}` (text) | `\{title}` |

In Blade, a directive starts with `@` and a component tag with `x-`. In the HTML syntax, a tag that starts with a capital letter belongs to the template; plain HTML is lowercase and is never touched, and `@` is plain text — so Alpine's `@click` and CSS `@media` need no escaping. A single brace is read only where it can be: in text and in attribute values, never inside `<script>`, `<style>`, a comment or a code attribute (`x-data="{ open: false }"`), and only when what is inside is a value — `{ color: red }` stays text. `\{` prints a brace. [Where a brace is read →](html-syntax.md#braces-where--is-read)

The full reference for each:

- [**The Blade syntax**](blade-syntax.md) — output, conditions, loops, components, `$attributes`, `@class` / `@style`, expressions, data, `@vite`.
- [**The HTML syntax**](html-syntax.md) — every tag, the `<If>` shorthands, `<Foreach limit>`, component tag names, and what is left alone in an `.html` file.

### Convert either way

A site written in one syntax can be rewritten in the other, in place:

```bash
devdojo convert my-site --to html             # Blade → the HTML syntax (--to studio works too)
devdojo convert my-site --to blade            # the HTML syntax → Blade
devdojo convert my-site --to blade --dry-run  # say what would change, touch nothing
```

A conversion is exact or it does not happen: every converted file is read back and compared token for token, the site is rendered before and after and every page must come out byte for byte the same, and one problem anywhere leaves every file untouched. Blade that needs real Laravel (`@auth`, `route()`) is refused on its way to the HTML syntax. The Laravel package renders Blade only, so converting to Blade is also how an `.html` site moves into a real Laravel app. [More on converting →](html-syntax.md#converting-between-the-two)

## Where a site keeps its files

One folder, `src/`, laid out flat:

```
my-site/
├── public/            # copied as they are (images, fonts)
└── src/
    ├── pages/         # one file per page: index.blade.php or index.html → /
    ├── components/    # components, with layouts/ and sections/ inside
    ├── css/           # the stylesheet @vite / <Vite> prints
    └── data/          # site.json, collections/, content/
```

Two older shapes are still read, so a site written before `src/` builds exactly as it did: the folder may be called `resources/`, and pages and components may each sit one level down, in `views/pages` and `views/components`. A site has one source folder, one pages folder and one components folder: when two candidates both hold files the newer one is read and the other is ignored.

The engine tells you about an older shape in a **notice**, never an error. Notices come back in their own `notices` list (`kind`, `code`, `message`, sometimes `file`), apart from `diagnostics`, so a build that fails on any diagnostic still passes; `devdojo lint` prints them to stderr and exits as it would have. Send `"options": {"notices": false}` to leave them out. The codes are `resources-folder`, `views-folder`, `folder-not-read`, `asset-path` and `double-braces` (an `.html` view still writing `{{ x }}` where `{x}` would do).

```bash
devdojo migrate my-site            # move it into src/ and write {{ x }} as {x}, in place
devdojo migrate my-site --dry-run  # say what would change, touch nothing
```

`migrate` also brings `.html` views up to single braces: `{{ x }}` becomes `{x}`, the old escape `@{{ x }}` becomes `\{\{ x }}`, and text that would now open a value (a code sample's `{title}`) is escaped as `\{title}`. Running it twice changes nothing. Single braces came in engine 1.1.0, the same version as `src/`: the version number did not change for them.

Like a conversion, a move is exact or it does not happen: the site is rendered before and after, every page must come out byte for byte the same, and one difference leaves every file where it was. A stylesheet named the old way (`@vite(['resources/css/site.css'])`) is rewritten with the move, and is found in `src/css/` even when it is not.

The Laravel package reads the same folders in the same order. A real Laravel app keeps `resources/`, which is Laravel's own name for it, and is never told otherwise.

## Data

Data comes from JSON and markdown files, bound by file name: `src/data/site.json` is `$site` (`site` in the HTML syntax), `src/data/collections/plans.json` is `$plans`, `src/data/content/post/*.md` is `$post`.

A collection can also be a **folder of entry files** — `src/data/collections/posts/<slug>.json`, one JSON object per entry, bound as `$posts` ordered by `date` descending then file name — for a blog too large to keep in one array file. The two forms bind identically; when a site has both for one name, the folder wins and the compile reports a collection error naming both paths.

## What a template can say

Output `{{ }}` / `{!! !!}` / `{{-- --}}` (`{x}` in the HTML syntax) · conditions · loops with the full `$loop` object, break and continue · components with props, bound attributes (`:items="$plans"`), `$attributes`, default and named slots · conditional `class` and `style` lists · expressions with `->` / `[]` access, `??`, comparisons, booleans, text joined with `.` — all with PHP semantics (truthiness, loose equality, stringification) · `@vite` rendered as its static projection (Tailwind Play CDN + inlined CSS).

No function calls, no assignments, no `@php`. Undefined variables are compile-time errors with file/line/col — `??` is the sanctioned default (`{{ $tagline ?? 'Welcome' }}`).

## The commands

**Start a site** and look at it. `new` downloads a template from GitHub and writes it into a new folder: the starter ([`devdojo-templates/starter`](https://github.com/devdojo-templates/starter)) unless `--template` names another — a bare name is a template of `devdojo-templates`, or give `owner/repo` or a repository's `https://github.com/…` address. `serve` shows it at `http://localhost:8000`, compiles a page when it is asked for, and reloads the browser when a file changes:

```bash
devdojo new my-site                     # or: devdojo new my-site --template blog
cd my-site
devdojo serve
```

`new` refuses a folder that already has something in it, before downloading anything, and a download that fails leaves nothing behind. `serve` takes `--port` and `--no-reload`. A page that does not compile is shown in the browser as `file:line:col` and what is wrong.

**Build** it as static files, ready to host anywhere:

```bash
devdojo build                                   # into dist/
devdojo build my-site --out public_html --url https://example.com
```

Every page at a clean address (`pricing.html` → `pricing/index.html`), one folder for each entry of a dynamic page (`blog/[posts.slug].html`), `public/` copied as it is, the 404 page as `404.html`, `robots.txt`, and `sitemap.xml` when the site's address is known (`--url`, or `"url"` in `src/data/site.json`). The pages get one stylesheet built by [Tailwind's standalone CLI](https://tailwindcss.com/blog/standalone-cli) in place of the browser script: it is downloaded once (v4.3.3, checked against its published checksum) into your cache folder. When it cannot be had, the pages keep the browser script and `build` says so. Nothing is written while any page has a problem; the problems are printed as `file:line:col  message`. A built site expects to be served from the root of its domain.

**Lint** a site directory — silence means every page compiles cleanly:

```bash
devdojo lint example
```

**Compile** speaks one JSON document on stdin, one on stdout — files in, files + deps + diagnostics out. No disk, no eval:

```bash
echo '{"files": {"src/pages/index.blade.php": "<h1>{{ $title }}</h1>", "src/data/site.json": "{}"}, "data": {"title": "Hi"}}' \
  | devdojo compile | jq -r '.files["index.html"]'
# <h1>Hi</h1>
```

A dynamic page — one template, one document per collection entry — renders in one request through `variants`: each variant names the entry, the data bound for that render alone (merged over `data`), and the output path, so the site's files are sent and parsed once rather than once per document:

```bash
echo '{"files": {"src/pages/post/[post.slug].blade.php": "<h1>{{ $post->title }}</h1>"}, "variants": [{"entry": "src/pages/post/[post.slug].blade.php", "data": {"post": {"title": "Hi"}}, "output": "post/hi/index.html"}]}' \
  | devdojo compile | jq -r '.files["post/hi/index.html"]'
# <h1>Hi</h1>
```

Or embed the engine directly from Go:

```go
result := framework.Compile(vfs.Dir("my-site"), []string{"src/pages/index.blade.php"}, framework.Options{})
```

The [`example/`](../example/) directory is the smallest useful site — a page, a layout, a component fed by collection data — with a `render.sh` that runs the whole pipeline. [`example-studio/`](../example-studio/) is the same site in the HTML syntax, and a test holds the two outputs identical.

## Repo layout

| Path | What it is |
|---|---|
| `blade/` | The Go engine: scanners, parser, renderer, expression evaluator. Stdlib-only, never panics on user input. `blade/scanner` reads Blade and `blade/htmlscan` reads the HTML syntax; `blade/convert` rewrites a site from one syntax to the other. |
| `laravel/` | The `devdojo/framework` Composer package that makes the same Blade files render in a real Laravel app. |
| `fixtures/` | The contract — conformance cases rendered by both engines. |
| `oracle/` | A minimal Laravel app that runs the fixtures through genuine Blade in CI. |
| `site/` | The one place that knows where a site keeps its files: `src/`, and the older shapes still read. |
| `migrate/` | Moves a site in an older shape into `src/`. |
| `cmd/devdojo/` | The CLI: `new`, `serve`, `build`, `compile`, `lint`, `convert`, `migrate`, `version`. |
| `starter/` | How `devdojo new` downloads a template from GitHub and writes it safely. The starter itself lives in [its own repository](https://github.com/devdojo-templates/starter), not in the binary. |
| `npm/` | The npm package `@devdojo/framework`: the `devdojo` command for npm, and the script the release uses to make one package per platform. |
| `composer.json` | Installs the Laravel package in `laravel/` from this repository (`composer require devdojo/framework`). |
| `docs/` | The two syntax guides ([Blade](blade-syntax.md), [HTML](html-syntax.md)), this page, and the design specs in [`docs/specs/`](specs/). |

