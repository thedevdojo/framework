# Folder collections and variant compiles (engine 0.2.1)

**Date:** 2026-09-16
**Status:** Implemented
**Origin:** Max AI Builder issue #1094 — a 1,071-post blog could not fit the
documented `collections/blog.json` + `pages/blog/[blog.slug].blade.php` shape
under the host's 2 MiB per-file limit, so the cloner wrote one Blade file per
post instead. The dialect needed a second storage form for a large collection,
and the host needed a way to compile a thousand documents from one template
without sending the site a thousand times.

## The two storage forms

| Form | Path | Decodes to | Order |
|---|---|---|---|
| File (default) | `resources/data/collections/NAME.json` | a JSON **array** | as authored |
| Folder | `resources/data/collections/NAME/<slug>.json` | one JSON **object** per file | `date` descending, then file name ascending |

Both bind `$NAME`, globally, with exactly the rules of
`2026-07-28-collections-global-scope-design.md`. Templates cannot tell the
forms apart; a `[NAME.slug].blade.php` dynamic page, a listing, a component
reading `$NAME` all render the same bytes from the same entries in either form.

Rules the loader (`blade/render/data.go`, `loadCollections`) enforces:

- **Direct children only.** `NAME/<file>.json` is an entry; `NAME/<dir>/x.json`
  is ignored, as any deeper path under `collections/` always was.
- **The file name is the slug.** An entry whose `slug` is a string that differs
  from its file name still binds — the data is the data — but is reported as a
  collection error, because every host tool addresses the entry by its file.
  An entry without a `slug` is fine.
- **Ordering.** `date` is compared as a string (ISO `YYYY-MM-DD` orders
  correctly); an entry whose `date` is missing or not a string sorts after
  every dated one. Ties break on file name ascending, so the order is total
  and stable.
- **Both forms present → the folder wins.** The file is ignored entirely —
  entries are never merged — and one collection error on the file's path
  names both paths and says to remove one.
- **A non-object entry** (an array, a scalar, malformed JSON) is a collection
  error and only that file is skipped.

The PHP tier (`laravel/src/SiteData.php`) applies the same rules, so the
conformance oracle renders `fixtures/data-collections-folder` through real
Blade to the same bytes.

## When a host uses the folder

The file form stays the default. The host convention (Max AI Builder) is that
a collection past **100 entries** is stored as a folder: its bundle import
splits a larger file automatically, its prompts tell the model to write a
folder past that size, and its content editor writes to whichever form a
collection already uses. The engine takes no position on the threshold — both
forms are first-class at any size.

## Variants: one template, many documents, one request

A host compiling a dynamic page needs one document per entry with that entry
bound over the collection's name. Before 0.2.1 that was one `compile` request
per entry, each carrying every source file and the whole collection: for the
originating blog, roughly 17 MB × 1,071 requests.

The protocol now takes `variants`:

```jsonc
{
  "files": { … },                       // sent once
  "data": { "entries": [ … ] },         // shared by every render in the request
  "variants": [
    { "entry": "resources/views/pages/post/[post.slug].blade.php",
      "data": { "post": { "slug": "hello", … } },   // this render only, wins over `data`
      "output": "post/hello/index.html" }           // default: the entry's usual output name
  ]
}
```

`render.CompileVariants` loads the site's data sources once, shares one parse
cache across every variant, and renders each variant with its data laid over
the shared page bindings (`variantBindings`) — the shared map is never
mutated. `entries` and `variants` combine in one request (entries render
first); a request naming neither discovers every page, as before. An `output`
that escapes the site root is refused exactly like an escaping entry.
`render.Compile` is now a thin wrapper that turns entries into data-less
variants.
