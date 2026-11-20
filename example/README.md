# DevDojo Framework example

The smallest useful Blade-dialect site: a page, a layout component with
`@props` + `{{ $slot }}`, a section receiving collection data through a bound
attribute, `$site` data, a `@foreach` with `$loop`, and `@vite` rendering its
static projection (Tailwind Play CDN + inlined CSS).

```
example/
├── render.sh                      # pack → compile → unpack helper (needs jq)
└── src/
    ├── css/site.css               # inlined by @vite('src/css/site.css')
    ├── data/
    │   ├── site.json              # → $site
    │   └── collections/features.json  # → $features
    ├── pages/index.blade.php
    └── components/
        ├── layouts/main.blade.php
        └── sections/feature-list.blade.php
```

The same site written in the HTML syntax (`<Layout>`, `<Foreach>`,
`<If>`, `<SectionsFeatureList />`) is in
[`../example-studio/`](../example-studio/); it renders the same bytes. Every
directive used here is explained in the
[Blade syntax guide](../docs/blade-syntax.md).

## 1. Build the binary

```bash
go build -o /usr/local/bin/devdojo ./cmd/devdojo   # from the repo root
```

## 2. Lint — the tier gate

```bash
devdojo lint example
```

Silence + exit 0 means every construct is inside the static subset. Add
`@auth` or `{{ route('home') }}` to a page and lint reports it as `[app-tier]`
— valid Blade that needs the site running as a real app — instead of an error.

## 3. Render

```bash
example/render.sh
open example/_out/index.html      # fully styled — Tailwind rides the Play CDN
```

`render.sh` is a thin wrapper around the real interface: one JSON document on
stdin (`{"files": {path: content}}`), one on stdout (`files`, `deps`,
`diagnostics`). That protocol is exactly how the DevDojo platform embeds the
engine — no disk, no eval, just bytes in and bytes out:

```bash
jq -n --rawfile page example/src/pages/index.blade.php \
  '{files: {"src/pages/index.blade.php": $page}}' \
  | devdojo compile | jq -r '.files["index.html"]'
```

(That one-liner omits the layout/component files, so the output also carries a
`component-missing` diagnostic — useful to see what error markers look like.)

## 4. The same files in real Laravel

The conformance oracle doubles as proof of the whole thesis — render the same
folder through genuine Blade:

```bash
cd oracle && php artisan framework:render ../example pages.index
```

Byte-for-byte identical to the Go engine's output.
