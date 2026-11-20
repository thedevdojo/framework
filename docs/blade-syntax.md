# The Blade syntax

DevDojo Framework compiles templates to HTML. It reads two syntaxes, and this is the
guide to the first: **Blade**, in files ending `.blade.php`. (The other is the
[DevDojo Studio syntax](studio-syntax.md), in `.html` files. A site can be
[converted](studio-syntax.md#converting-between-the-two) from one to the other
at any time.)

What DevDojo Framework reads is a **closed subset of Laravel Blade**: the part of
Blade that turns data into HTML, and none of the part that runs PHP. Everything
in this guide prints byte for byte what a real Laravel app prints from the same
files — the conformance suite renders every case with both and compares. So a
DevDojo Framework site is a slice of a Laravel app: move the files into one and they
keep working.

```blade
<x-layouts.main title="Pricing">
    <h1>{{ $site->name }}</h1>

    @foreach ($plans as $plan)
        <div @class(['plan', 'plan-featured' => $plan->featured])>
            {{ $loop->iteration }}. {{ $plan->name }} — {{ $plan->price }}
        </div>
    @endforeach

    <x-button href="/signup" class="btn-lg">Get started</x-button>
</x-layouts.main>
```

## A site

Everything a site is made of lives in one folder, `src/`:

```
my-site/
├── public/                          # copied as they are (images, fonts)
└── src/
    ├── pages/
    │   ├── index.blade.php          # → /
    │   └── about.blade.php          # → /about
    ├── components/
    │   ├── button.blade.php         # <x-button>
    │   ├── layouts/main.blade.php   # <x-layouts.main>
    │   └── sections/hero.blade.php  # <x-sections.hero>
    ├── css/site.css                 # the stylesheet @vite prints
    └── data/
        ├── site.json                # → $site
        ├── collections/plans.json   # → $plans
        └── content/post/*.md        # → $post
```

A site written before `src/` keeps working: the folder may still be called
`resources/`, and pages and components may still sit one level down, in
`views/pages` and `views/components` (a Laravel app's own layout). The engine
says so in a notice and `devdojo migrate my-site` moves it. See
[the README](../README.md#where-a-site-keeps-its-files).

**Pages** live under `src/pages/` and the path is the address:

| Address | File |
|---|---|
| `/` | `pages/index.blade.php` |
| `/about` | `pages/about.blade.php`, else `pages/about/index.blade.php` |
| anything not found | `pages/404.blade.php`, when there is one |

## Printing values

| Write | Prints |
|---|---|
| `{{ $post->title }}` | the value, escaped for HTML (`& < > " '` become entities) |
| `{!! $post->body !!}` | the value as it is — for HTML you trust |
| `{{-- a note --}}` | nothing; a comment that never reaches the page |
| `@{{ name }}` | the text `{{ name }}`, for a page that carries another template language |
| `@@click` | the text `@click` |

A value prints the way PHP's `echo` prints it:

| Value | Prints |
|---|---|
| `null`, `false` | nothing |
| `true` | `1` |
| `1.0`, `2.5` | `1`, `2.5` |
| a list or an object | an error — loop over it or read a field instead |

## Conditions

```blade
@if ($plan->price == 0)
    Free
@elseif ($plan->featured)
    Most popular
@else
    {{ $plan->price }}
@endif
```

A directive on a line of its own leaves no blank line behind, exactly as in
Laravel.

## Loops

```blade
@foreach ($plans as $plan)
    <li>{{ $plan->name }}</li>
@endforeach

@foreach ($site->links as $label => $href)
    <a href="{{ $href }}">{{ $label }}</a>
@endforeach
```

Inside a loop, `$loop` describes where you are:

| | |
|---|---|
| `$loop->index` | 0, 1, 2 … |
| `$loop->iteration` | 1, 2, 3 … |
| `$loop->first`, `$loop->last` | true on the first / last item |
| `$loop->count` | how many items there are |
| `$loop->even`, `$loop->odd` | by `iteration` |

`@break` leaves the loop and `@continue` skips to the next item. Both take an
optional condition, which is how a template shows the first few of a list:

```blade
@foreach ($posts as $post)
    @break($loop->index >= 3)
    <x-post-card :post="$post" />
@endforeach
```

Looping over an object walks its fields in the order the JSON file lists them.

## Components

A component is a file under `src/components/`, used as a tag. Dots
in the tag are folders:

| Tag | File |
|---|---|
| `<x-button>` | `components/button.blade.php` |
| `<x-sections.hero>` | `components/sections/hero.blade.php` |
| `<x-layouts.main>` | `components/layouts/main.blade.php` |

```blade
{{-- src/components/sections/hero.blade.php --}}
@props(['heading' => 'Hello', 'cta' => 'Learn more'])
<section class="hero">
    <h1>{{ $heading }}</h1>
    <a href="/signup">{{ $cta }}</a>
</section>
```

```blade
<x-sections.hero heading="Build sites with Blade" cta="Get started" />
```

**`@props`** names what the component accepts and gives each a default. A
default is a literal: text, a number, `true`, `false`, `null`, or a flat list
(`'items' => []`). A name with no default (`@props(['heading'])`) is required.

**Attributes** on the tag fill the props:

| Write | Passes |
|---|---|
| `heading="Hi"` | the text `Hi` |
| `:items="$plans"` | a value — the result of the expression |
| `wide` | `true` |

A dashed attribute is read back in camelCase: `logo-text="Hi"` is `$logoText`.

**Slots.** What sits between a component's tags is its slot, printed with
`{{ $slot }}`. A named slot is filled with `<x-slot:name>` and printed as a
variable of that name:

```blade
{{-- src/components/layouts/main.blade.php --}}
@props(['title' => 'Untitled'])
<html>
<head>
    <title>{{ $title }} — {{ $site->name }}</title>
    {{ $head ?? '' }}
</head>
<body>
    {{ $slot }}
</body>
</html>
```

```blade
<x-layouts.main title="Pricing">
    <x-slot:head>
        <meta name="description" content="{{ $site->tagline }}">
    </x-slot>
    <h1>Pricing</h1>
</x-layouts.main>
```

**Layouts are components.** There is no separate layout construct: a layout is
a component that wraps the page in `{{ $slot }}`, as above.

A component may use other components, up to 16 deep.

### `$attributes`

Any attribute a component does not name in `@props` lands in `$attributes`, so
a caller can add a class or a `data-` attribute without the component planning
for it:

```blade
{{-- src/components/button.blade.php --}}
@props(['variant' => 'primary', 'href' => '#'])
<a href="{{ $href }}" {{ $attributes->merge(['class' => 'btn btn-' . $variant]) }}>{{ $slot }}</a>
```

```blade
<x-button href="/signup" class="btn-lg" data-track="cta">Get started</x-button>
```

```html
<a href="/signup" class="btn btn-primary btn-lg" data-track="cta">Get started</a>
```

`{{ $attributes }}` prints them all. These methods work, with Laravel's
behavior: `merge`, `class`, `style`, `only`, `except`, `has`, `hasAny`,
`missing`, `get`, `first`, `whereStartsWith`, `whereDoesntStartWith`,
`thatStartWith`, `isEmpty`, `isNotEmpty`, `all`, `toArray`, `getAttributes`.

### `@class` and `@style`

Print a `class` or `style` attribute from a list. A plain entry is always
included; an entry with `=>` is included when its condition holds:

```blade
<div @class(['plan', 'plan-featured' => $plan->featured])>
<p @style(['color: red', 'font-weight: bold' => $important])>
```

```html
<div class="plan plan-featured">
<p style="color: red; font-weight: bold;">
```

## Expressions

Everything between `{{ }}`, inside a directive's brackets, and in a `:bound`
attribute is an expression. The whole language:

| | |
|---|---|
| Variables | `$name` |
| Reading into a value | `$post->title`, `$post->author->name`, `$items[0]`, `$row['key']` |
| A default | `$tagline ?? 'Welcome'` |
| Comparing | `==`, `!=`, `<`, `>`, `<=`, `>=` |
| Combining | `&&`, `\|\|`, `!`, brackets |
| Joining text | `'Hi ' . $name` |
| Literals | `'text'`, `"text"`, `12`, `2.5`, `true`, `false`, `null`, `['a', 'k' => $v]` |

That is all of it. There is no math, no `? :`, no function calls and no
assignment — a template reads values and compares them.

Values behave as they do in PHP:

- **Truthiness.** `0`, `''`, `'0'`, `null` and an empty list are false.
  `'false'` is true. An object is always true, even an empty one.
- **Loose equality.** `'1' == 1` is true; `'abc' == 0` is false.
- **Objects and lists.** A JSON object is read with `->` (`$site->name`); a
  JSON array is read with `[]` (`$plans[0]`). Reading an object with `[]` is an
  error.

**An undefined variable is an error**, with the file, line and column — it is
never printed as an empty string. So is a field an object does not have. `??`
is the way to say "this may be missing":

```blade
{{ $tagline ?? 'Welcome' }}
{{ $post->subtitle ?? '' }}
```

## Data

Data comes from files, and each is bound by its name:

| File | Variable | |
|---|---|---|
| `src/data/site.json` | `$site` | one object, for the whole site |
| `src/data/collections/plans.json` | `$plans` | a JSON array, in the order written |
| `src/data/collections/posts/<slug>.json` | `$posts` | a folder of entries, one object per file, newest `date` first |
| `src/data/content/post/*.md` | `$post` | markdown files, in file-name order |

A markdown entry carries its front-matter fields, plus `content` (the rendered
HTML — print it with `{!! $entry->content !!}`) and `link` (its address).

All of it is available everywhere: pages, layouts and components. Inside a
component, an attribute that was passed wins over a collection of the same
name; a `@props` default does not.

## Stylesheets: `@vite`

```blade
@vite(['src/css/site.css'])
```

prints the Tailwind v4 Play CDN script and the stylesheet inline, so a page is
styled with no build step. In a Laravel app with real Vite the same line is
Laravel's own. Only CSS entries are compiled; a JavaScript entry is skipped and
reported as `app-tier` (see below) — use a plain `<script>` tag.

## What is outside the subset

Blade that needs a running Laravel app is not an error and is not guessed at.
It is reported as **`app-tier`**: valid Blade, to be run by a real app.

- any other directive Laravel knows: `@auth`, `@include`, `@php`, `@csrf`, …
- function and method calls: `route('home')`, `$posts->count()`
- `?->`, math, `? :`, assignments, closures

A directive nobody knows (`@media` in inline CSS, an `@handle` in a sentence)
is printed as it was written, as Laravel prints it. `devdojo lint` still
lists it as `app-tier`.

## Checking a site

```bash
devdojo lint my-site
```

prints one line per finding — `file:line:col [kind] message` — and nothing at
all when the site is clean.

| Kind | Means | Exit code |
|---|---|---|
| `error` | a mistake: an unclosed `@if`, an undefined variable, a missing component | 1 |
| `app-tier` | valid Blade that needs a real Laravel app | 0 |
| `warning` | a hint: an attribute the component never declared (probably a typo) | 0 |

A missing component also leaves a marker in the page —
`<!-- devdojo:missing component "sections.hero" -->` — so it can be found
in the output. The engine never crashes on a template, however broken.

## In a real Laravel app

The [`devdojo/framework`](../laravel/) Composer package points Laravel at a
site folder and binds the same data, so the same files render through genuine
Blade. Nothing moves and nothing is rewritten. From there the `app-tier` list
above is simply available.

## More

- [The DevDojo Studio syntax](studio-syntax.md) — the same templates, written
  as tags.
- [`example/`](../example/) — a small complete site in this syntax.
- [`docs/specs/`](specs/) — the design documents, for the exact rules behind
  each section here.
