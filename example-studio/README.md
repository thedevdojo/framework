# DevDojo Framework example — DevDojo Studio syntax

[`../example/`](../example/) again, written with tags instead of Blade
directives. Same data, same CSS, **the same rendered bytes** — a test
(`TestExamplesRenderAlike`) holds the two outputs identical.

Nobody typed these views: they are what `devdojo convert example --to studio`
writes, and a second test (`TestExampleStudioIsTheConversion`) keeps them that
way. Run it the other way (`--to blade`) and you are back at `../example/`.

```
example-studio/
└── resources/
    ├── css/site.css               # inlined by <Vite src="resources/css/site.css" />
    ├── data/
    │   ├── site.json              # → site
    │   └── collections/features.json  # → features
    └── views/
        ├── pages/index.html
        └── components/
            ├── layouts/main.html            # <Layout src="main.html">
            └── sections/feature-list.html   # <SectionsFeatureList />
```

Side by side:

| This folder (`.html`) | `../example/` (`.blade.php`) |
|---|---|
| `<Layout src="main.html" title="…">` | `<x-layouts.main title="…">` |
| `{{ site.name }}` | `{{ $site->name }}` |
| `<SectionsFeatureList :items="features"/>` | `<x-sections.feature-list :items="$features"/>` |
| `<Props heading="Features" items />` | `@props(['heading' => 'Features', 'items' => null])` |
| `<If items>` … `<Else>` … `</If>` | `@if ($items)` … `@else` … `@endif` |
| `<Foreach items="item">` | `@foreach ($items as $item)` |
| `{{ loop.iteration }} of {{ loop.count }}` | `{{ $loop->iteration }} of {{ $loop->count }}` |
| `<Slot />` | `{{ $slot }}` |

## Run it

```bash
go build -o /usr/local/bin/devdojo ./cmd/devdojo   # from the repo root
devdojo lint example-studio                            # silence = clean
example/render.sh example-studio                           # writes example-studio/_out/index.html
```

The file extension picks the syntax, so nothing else changes: the same
`compile` protocol, the same `lint`, the same data files. Every tag is
explained in the [DevDojo Studio syntax guide](../docs/studio-syntax.md).

One difference from the Blade example: step 4 there (rendering the same files
in real Laravel) does not apply here — the Laravel package reads Blade only.
To take this site to Laravel, convert it first:

```bash
devdojo convert example-studio --to blade --dry-run   # show what would change
devdojo convert example-studio --to blade             # rewrite the views in place
```
