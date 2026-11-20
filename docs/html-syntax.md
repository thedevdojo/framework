# The HTML syntax

DevDojo Framework compiles templates to HTML. It reads two syntaxes, and this is the
guide to the second: the **HTML syntax**, in files ending `.html`.
(The other is [Blade](blade-syntax.md), in `.blade.php` files.)

The HTML syntax says what Blade says with tags instead of directives:
`<Foreach posts="post">` for `@foreach ($posts as $post)`, `{{ post.title }}`
for `{{ $post->title }}`, `<SectionsHero />` for `<x-sections.hero />`. It is
for people who know HTML and nothing else — a template is HTML with a few
extra tags. The tags follow DevDojo Static's (`<Layout>`, `<Include>`,
`<Foreach>`).

The two syntaxes are one language. They share a parser and a renderer, print
the same bytes from the same data, and a site can be
[converted](#converting-between-the-two) from either one to the other.

```html
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

## Which syntax a file is in

The file's extension decides, one file at a time:

| Extension | Syntax |
|---|---|
| `.blade.php` | Blade |
| `.html` | the HTML syntax |

The site's folders, its data files and its addresses are the same in both —
see ["A site"](blade-syntax.md#a-site) and ["Data"](blade-syntax.md#data) in
the Blade guide, reading `.html` for `.blade.php` and `site` for `$site`.

Pages are discovered under `src/pages/` in both (`about.html` and
`about.blade.php` both serve `/about`; having both is a `page-collision`
error). A site can mix them: an `.html` page can use a `.blade.php` layout or
component. The reverse is not true — a Blade `<x-card>` finds only
`card.blade.php`, so a Blade page stays something real Laravel can render.

## Cheat sheet

| The HTML syntax | Blade |
|---|---|
| `{{ post.title }}` | `{{ $post->title }}` |
| `{!! post.body !!}` | `{!! $post->body !!}` |
| `{{-- note --}}` | `{{-- note --}}` |
| `<If condition="plan == 'pro'">` … `</If>` | `@if ($plan == 'pro')` … `@endif` |
| `<ElseIf condition="…">`, `<Else>` | `@elseif (…)`, `@else` |
| `<Foreach posts="post">` … `</Foreach>` | `@foreach ($posts as $post)` … `@endforeach` |
| `<Foreach posts="i, post">` | `@foreach ($posts as $i => $post)` |
| `<Foreach posts="post" limit="3">` | `@foreach ($posts as $post)` `@break($loop->index >= 3)` |
| `<Break />`, `<Continue condition="…" />` | `@break`, `@continue(…)` |
| `<Layout src="main.html" title="Home">` | `<x-layouts.main title="Home">` |
| `<Include src="button.html" />` | `<x-button />` |
| `<Button>Save</Button>` | `<x-button>Save</x-button>` |
| `<SectionsHero heading="Hi" :items="posts" />` | `<x-sections.hero heading="Hi" :items="$posts" />` |
| `<Slot name="head">…</Slot>` (inside a component tag) | `<x-slot:head>…</x-slot>` |
| `<Slot />` (inside a component file) | `{{ $slot }}` |
| `<Slot name="head" />` (inside a component file) | `{{ $head ?? '' }}` |
| `<Props title="Untitled" :count="3" badge />` | `@props(['title' => 'Untitled', 'count' => 3, 'badge' => null])` |
| `<Vite src="src/css/site.css" />` | `@vite(['src/css/site.css'])` |
| `<a {{ class(['btn', 'on' => active]) }}>` | `<a @class(['btn', 'on' => $active])>` |
| `<Card title="Hi {{ name }}" />` | `<x-card :title="'Hi ' . $name" />` |

**The one rule to remember: a tag that starts with a capital letter belongs to
the template.** Plain HTML is lowercase and is never touched.

## Values and conditions

Inside `{{ }}`, `condition="…"` and any `:bound` attribute:

- A bare word is a variable: `title`. A dot reads into it: `post.author.name`.
  `posts[0]` and `??` work as in Blade (`{{ tagline ?? 'Welcome' }}`).
- `=`, `==` and `===` all mean "equals"; `!=` and `!==` mean "not equals";
  `<`, `>`, `<=`, `>=` compare.
- `and`, `or`, `not` are accepted beside `&&`, `||`, `!`.
- Text goes in quotes: `'pro'`. `true`, `false`, `null` and numbers are
  themselves.
- A dot with a space on each side joins text: `'Hi ' . name`. (A dot with no
  spaces reads into a value: `post.title`.) Lists and objects cannot be joined.
- Blade spelling still works (`$post->title`), so a pasted expression is fine.

That is the whole expression language — the same closed subset Blade files
get. No math, no `? :`, no function calls; use `<If>` and `{{ }}` side by side
instead (`class="btn btn-{{ variant }}"`).

`{{ class([...]) }}` and `{{ style([...]) }}` are Blade's `@class` and
`@style`: inside a plain HTML tag, they print the whole attribute from a list
of names, each kept when its condition holds —
`<a {{ class(['btn', 'btn-on' => active]) }}>`.

`<If>` also has shorthands, so the common cases need no expression at all:

| Write | Means |
|---|---|
| `<If featured>` | `featured` is truthy |
| `<If post.featured>` | same, reading into `post` |
| `<If not="draft">` | `draft` is falsy |
| `<If plan="pro">` | `plan` equals the text `pro` (`true`, `false`, `null` and numbers keep their type) |
| `<If :plan="account.plan">` | `plan` equals another value |
| `<If featured plan="pro">` | both (several attributes are ANDed) |

`<ElseIf>`, `<Break />` and `<Continue />` take the same attributes. `<Else>`
and `<ElseIf>` are dividers inside an `<If>`, not wrappers — they have no
closing tag (`<Else />` is accepted). `condition` and `not` are reserved
attribute names on these tags.

## Loops

`<Foreach posts="post">`: the attribute name is the list, its value is what
each item is called. `posts="i, post"` (or `"i => post"`) also names the key.
The list can be a path: `<Foreach post.tags="tag">`.

The long form from DevDojo Static works too: `<Foreach collection="posts"
as="post" key="i">`. With no `as`, each item takes the collection's own name
(`<Foreach collection="posts">` … `{{ posts.title }}`), as Static does.

`limit="3"` (alias `count`) shows the first three. It is a true slice:
`loop.count` is 3 and `loop.last` is true on the third. Blade has no limit; its
nearest spelling is a `@break` as the loop's first line, which is the same loop
whenever nothing inside asks how long the list is.

`loop` is Blade's `$loop`: `loop.index`, `loop.iteration`, `loop.first`,
`loop.last`, `loop.count`, `loop.even`, `loop.odd`.

## Layouts, includes and components

All three are the same thing — a component file — reached three ways:

- `<Layout src="main.html">` → `src/components/layouts/main.html`
- `<Include src="button.html">` → `src/components/button.html`
- `<Button>` → `src/components/button.html`

`src` is a file path below that folder (`src="marketing/wide.html"`); the
extension is optional. `<layout>` and `<include>` may be written lowercase, the
DevDojo Static spelling. Every other attribute is passed to the file exactly as
on a component tag.

**Component tag names.** The tag is the file's path in PascalCase. Each capital
starts a new word, and the gap between two words is either a folder or a dash;
folders are tried first:

| Tag | Looks for (first match wins) |
|---|---|
| `<Button>` | `button.html` |
| `<SectionsHero>` | `sections/hero.html`, then `sections-hero.html` |
| `<PricingTable>` | `pricing/table.html`, then `pricing-table.html` |
| `<Sections.Hero>` | `sections/hero.html` only — a dot always means a folder |
| `<FAQSection>` | `faq/section.html`, then `faq-section.html` |

Each candidate is tried as `.html`, then `.blade.php`. A missing component's
error lists where it looked. `devdojo lint` warns when a tag matches more
than one file.

**Attributes.** `heading="Hi"` passes text. `:items="posts"` passes a value.
A bare `open` passes `true`. `{{ }}` works inside a text attribute:
`title="Hi {{ name }}"` is exactly `:title="'Hi ' . name"`, so the result is
always text (`data-id="{{ id }}"` passes `"7"`, not the number). Anything the
component does not declare in `<Props>`
lands in `attributes`, as in Blade: `<div {{ attributes }}>` or
`{{ attributes.merge(['class' => 'btn']) }}`.

**`<Props>`** declares what a component accepts and must be the first thing in
its file: `<Props title="Untitled" badge :count="3" logo-text="Hi" />` — a text
default, a prop that is empty until passed, a typed default after a colon, and
a dashed name read back as `logoText`. A text default that needs both kinds of
quotes writes one of them as `&quot;` or `&#39;` (and a literal `&quot;` as
`&amp;quot;`): `<Props say="can't say &quot;no&quot;" />`.

A bare name (`badge`) is **optional**: leave it off the tag and it is empty
(`null`), so `<If badge>` is simply false. This is the one place the HTML
syntax does not mirror Blade word for word — Blade's bare `@props(['badge'])`
makes the prop required and a tag without it fails on an undefined variable.
`<Props badge />` is `@props(['badge' => null])`.

**Slots.** Content between a component's tags is its default slot; the file
prints it with `<Slot />` (or `{{ slot }}`). `<Slot name="head">…</Slot>`
inside the component's tags fills a named slot; the file prints it with
`<Slot name="head" />`, which is empty when nothing filled it.

## What is left alone in an `.html` file

- **`@` is plain text.** No directives exist here, so Alpine's `@click`, CSS
  `@media` and e-mail addresses need no escaping. (`@{{ x }}` still prints a
  literal `{{ x }}`.)
- **Lowercase tags**, including `<slot>`, SVG's `<linearGradient>` and custom
  elements.
- **Shouted HTML.** A real HTML element in ALL CAPS (`<BR>`, `<TABLE>`) is
  HTML, not a component.
- **HTML comments.** Everything inside `<!-- -->` is copied through untouched.
- **`<script>` and `<style>`.** A capitalized tag inside them is code
  (`a<B>c`), not a component. `{{ }}` and the built-in tags still work there,
  so a JSON-LD block can loop.
- **A half-typed tag** costs only its `<`; the rest of the file still renders.

Reserved tag names (any capitalization): `If`, `ElseIf`, `Else`, `Foreach`,
`Break`, `Continue`, `Layout`, `Include`, `Slot`, `Props`, `Vite`, plus
`Script` and `Style`. A component file cannot be reached by one of these names
as a tag; `<Include src>` still reaches it.

## Diagnostics

Same shape, same positions, worded the way the file is written: `unclosed
<If>`, `<Break> outside a <Foreach>`, `undefined variable titel` (no `$`),
`component <SectionsHero> not found — looked for …`.

An `.html` file has **no app tier**. In Blade, `{{ route('home') }}` is
`app-tier` — real Laravel can run it. Real Laravel cannot read an `.html`
view, so there the same construct is an `error`.

One inherited behavior worth knowing: a tag that ends a line swallows that
line break, exactly as a Blade directive does. It is what keeps the two
syntaxes byte-identical; in HTML output it is invisible.

## Converting between the two

```bash
devdojo convert my-site --to html             # Blade → the HTML syntax
devdojo convert my-site --to blade            # the HTML syntax → Blade
devdojo convert my-site --to blade --dry-run  # say what would change, touch nothing
```

(`--to studio`, the HTML syntax's older name, still works.)

Every view under `src/` written in the other syntax is rewritten
in place — `about.blade.php` becomes `about.html`, or the reverse. Data, CSS
and everything else stay as they are. With no directory the same thing speaks
JSON, for a host that keeps its files elsewhere. There `"to"` is `"html"` or
`"blade"`; `"studio"`, the value hosts have always sent, means the HTML syntax
too and is kept for historical reasons:

```bash
echo '{"to": "html", "files": {"src/pages/index.blade.php": "@if ($on) yes @endif"}, "data": {"on": true}}' \
  | devdojo convert
# {"engine":"1.1.0","files":{"src/pages/index.html":"<If on> yes </If>"},"removed":["src/pages/index.blade.php"],"diagnostics":[]}
```

**Exact, or not at all.** The two syntaxes are two spellings of one token
stream, so converting a file is printing its tokens the other way. Nothing is
evaluated and nothing is guessed, and three checks stand behind the result:

1. Each converted file is scanned again and its tokens compared with the
   original's.
2. The site is rendered before and after with the same data. Every page must
   come out byte for byte the same.
3. Blade that needs real Laravel is refused on its way to the HTML syntax,
   which has no Laravel behind it.

One problem anywhere and **no file is written**: a site left half in each
syntax would have pages looking for components that moved. Each problem is
printed as `file:line:col message` and the exit code is 1.

**Blade → HTML** picks the shortest spelling that reads back the same:
`<If post.featured>` over `<If condition="post.featured">`, `<SectionsHero>`
over `<Include src="…">`, `title="Hi {{ name }}"` over
`:title="'Hi ' . name"`, `<Foreach posts="post" limit="3">` over a leading
`<Break>`. Text that only Blade had to escape goes back to plain: `@@click` is
`@click` again.

**HTML → Blade** writes Blade that real Laravel prints the same way — that is
the reason to convert. An `.html` file has no directives, so its text can hold
things Blade would act on. Each is defused:

| In the `.html` file | Written to Blade as | Why |
|---|---|---|
| `@click`, `@media`, `@handle` | `@@click`, `@@media`, `@@handle` | Blade's own escape for an at-sign |
| `@media (min-width: 1px)` | `{!! '@' !!}media (min-width: 1px)` | Laravel drops the space after an escaped `@@word (` |
| `<x-card>` as text | `{!! '<' !!}x-card>` | Blade would read a component tag |
| `yes</If>` | `yes{{ '' }}@endif` | `yes@endif` is not a directive; the empty echo holds them apart |
| `<Foreach posts="post" limit="3">` | `@foreach ($posts as $post)` then `@break($loop->index >= 3)` | Blade has no limit |
| `title="Hi {{ name }}"` | `:title="'Hi ' . $name"` | the same joined text |
| `<Props badge />` | `@props(['badge' => null])` | an optional prop |
| `{{ class() }}` | `@class` | Laravel fails on `@class()` |

Converting back to the HTML syntax takes all of that out again, so
HTML → Blade → HTML returns the file as it was written — bar an escape the
file never needed (`&amp;` in a `<Props>` default comes back as `&`).

**What is refused**, with the file and line:

- *To HTML:* a directive Laravel compiles and this engine does not (`@auth`,
  `@include`, `@php`, …), a function call (`route('home')`), a nested `@props`
  default. A directive nobody compiles (`@media`, `@handle`) is plain text and
  converts.
- *To Blade:* a `<Foreach limit>` whose loop reads `loop.last` or `loop.count`
  (a `@break` would leave them describing the whole list), or whose limit
  reads the loop's own item.
- *Either way:* a tag the scanner could not act on (`<Layout>` with no `src`),
  a value holding both kinds of quotes where one attribute cannot, and a
  target file that already exists (`about.html` beside `about.blade.php`).

A mistake that both syntaxes can spell is carried across as it is: an unclosed
`<If>` becomes an unclosed `@if`. Run `devdojo lint` first.

**What a round trip changes.** Nothing that renders. Blade → HTML → Blade
tidies spelling (`@if($a)` → `@if ($a)`, `@vite('a.css')` →
`@vite(['a.css'])`, blank lines inside `@props`), and one thing has no HTML-syntax
spelling: Blade's *required* prop. `@props(['items'])` becomes
`<Props items />`, which is optional, and comes back as
`@props(['items' => null])`. Every page that passed the prop renders the same;
a page that forgot it now gets an empty value where Blade would have stopped
with an error.

The render check uses the data the site ships with (`src/data/`), plus
whatever the JSON form passes as `data`. Hand it what the site is normally
rendered with, so the check walks the same branches.

## How it is built

One engine, two scanners. `blade/htmlscan` reads an `.html` file and emits the
**same token stream** `blade/scanner` emits for the equivalent Blade. The
parser, renderer and expression evaluator are shared and unaware of the
difference, apart from wording diagnostics (`blade/render/dialect.go`). The two
syntaxes therefore cannot drift: there is only one implementation of what an
`<If>` or a slot *does*.

Friendly expressions are rewritten to Blade's at scan time
(`htmlscan.Expr`: `post.title` → `$post->title`, `=` → `==`, `and` → `&&`).

Proof, since the PHP oracle cannot read an `.html` view:

- `htmlscan.TestScanMatchesBlade` — each construct scans to the tokens of its
  Blade twin.
- `fixtures/html-*` with a `"twin"` — the HTML-syntax port of an oracle-verified
  Blade fixture must expect that fixture's exact bytes (enforced by the
  harness).
- `example-studio/` renders byte-identical to `example/`, and is exactly what
  `devdojo convert example --to html` writes
  (`TestExampleStudioIsTheConversion`).
- Fuzzers: `FuzzScan` (plain HTML comes back unchanged), `FuzzExpr`,
  `FuzzParseHTML`, `FuzzCompileHTML`.

The converter (`blade/convert`) has its own:

- `fixtures.TestFixturesConvert` — every fixture is converted to the other
  syntax and back, and must still render its `expected/` bytes.
- `fixtures/convert-hazards` — a page of text Blade would act on (`@click`,
  `@media (…)`, `<x-card>`, `@{{ }}`, an empty class list), written in the
  HTML syntax, converted, and rendered by **real Laravel**: it prints what the
  author wrote.
- `convert.TestCorpus` (opt-in, `DEVDOJO_CORPUS=<dir of sites>`) — converts
  each site to the HTML syntax and back twice over, requiring identical
  renders and a second trip that changes nothing.
- `convert.FuzzFile` — on arbitrary input the converter may refuse, but what
  it converts must render the same bytes.

## Not built yet

- **The Laravel tier.** `laravel/` and the oracle render Blade only. An
  `.html` site moves into a real Laravel app by converting it first
  (`devdojo convert <dir> --to blade`).
- **A required prop.** `<Props badge />` is optional; there is no way to say
  "fail if this is missing".
- **DevDojo Static's single-brace `{title}` tokens.** Double braces were kept:
  single braces collide with CSS, JavaScript and Alpine in the same file.

## More

- [The Blade syntax](blade-syntax.md) — the same templates, written as Blade,
  and the guide to a site's folders and data.
- [`example-studio/`](../example-studio/) — a small complete site in this
  syntax.
- [`docs/specs/`](specs/) — the engine's design documents.
