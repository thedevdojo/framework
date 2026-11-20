# One repo, three ways in: `devdojo new`, `devdojo serve`, npm and Composer

**Status:** Decided (Tony, 2026-11-20: "lock it in and build it"). Built the
same day: the root `composer.json` (§4), `devdojo new` and `devdojo serve`
(§6). Not built yet: the npm package (§5), `devdojo build`, a template through
`new`. "Where it stands" at the end has the list.
**Date:** November 20, 2026

Related: [`2026-11-20-reactivity-direction.md`](2026-11-20-reactivity-direction.md),
[`2026-11-20-framework-export-design.md`](2026-11-20-framework-export-design.md).

---

## 1. What we want

One repository, `github.com/thedevdojo/framework`, that a person reaches three
ways:

```bash
npm install -g @devdojo/framework   # the `devdojo` command
composer require devdojo/framework  # Framework inside a Laravel app
```

and a plain download of the binary for anyone with neither.

With the command installed:

```bash
devdojo new my-site     # a new folder with a starter site in it
cd my-site
devdojo serve           # the site at http://localhost:8000, reloading as you edit
devdojo build           # the site as static files, ready to host anywhere
```

One version number covers all of it. A tag `vX.Y.Z` is the binary's release,
the Composer package's version and the npm package's version.

## 2. Decided

Talked through with Tony on 2026-11-20 and confirmed by him the same day.

1. **It lives in this repo**, not a second one.
2. **`new` and `serve` are commands of the `devdojo` binary**, written in Go
   beside `compile`, `lint`, `convert` and `migrate`. They are not a Node
   program.
3. **The npm package is an installer.** `@devdojo/framework` puts the right
   `devdojo` binary on a person's machine and nothing more.
4. **The Laravel package is installed from this repo too**, through a
   `composer.json` at the root. No split repo (§4).
5. **The starter is `github.com/devdojo-templates/starter`**, the same
   template DevDojo lists first. `devdojo new` writes a copy of it that is
   kept inside the binary (§6).

### What a site looks like

`public/` and `src/` sit side by side at the root of the project. Nothing
else is needed:

```
my-site/
  public/              served as it is: images, js, favicon.svg, robots.txt
  src/
    pages/             one file for each page (index.html is the home page)
      blog/[posts.slug].html   one page for each entry of a collection
    components/
      layouts/main.html        the page frame
      sections/                the sections pages are made of
    data/
      site.json                values every page can read
      collections/*.json       lists: posts, plans, features
    css/site.css       the theme
  dist/                what `devdojo build` writes (not built yet)
```

`public/images/logo.png` is served at `/images/logo.png`. `src/` is never
served as it is: its pages are compiled.

## 3. Why the binary, and not a Node CLI

- **One command, one name.** `devdojo` is already the engine's command. A
  Node CLI of the same name would have to pass `lint`, `convert` and
  `migrate` through to the binary and keep up with every new one.
- **No second copy of the engine's rules.** The Node CLI that exists today
  (§7) carries JavaScript ports of how pages are found, how dynamic pages fan
  out and how a site is exported. Each port is something that can drift. In
  Go, `serve` and `build` call the code `compile` already runs.
- **No Node needed.** A person with Homebrew, or only `curl`, gets the same
  tool.
- **The releases already exist.** goreleaser builds
  `devdojo_<os>_<arch>.tar.gz` for macOS, Linux and Windows on every tag, with
  `checksums.txt`.

## 4. Composer: a `composer.json` at the root

Production cannot install a package that sits in a subfolder of a repository:
Composer reads `composer.json` at the root. Pocketknife solved that with a
second repo (`thedevdojo/pocketknife-laravel`, a subtree split of `laravel/`,
tagged on its own, which is why the package was `0.3.0` while the engine was
`1.0.0`).

Instead:

- A root `composer.json` named `devdojo/framework`, with what
  `laravel/composer.json` says today, and its paths pointed into `laravel/`
  (`"Devdojo\\Framework\\": "laravel/src/"`, the service provider under
  `extra.laravel.providers`).
- A `.gitattributes` with `export-ignore` for everything a Laravel app does
  not need (the Go source, `fixtures/`, `oracle/`, `example*/`, `docs/`,
  `npm/`, `starter/`), so a Composer install downloads the PHP package and
  little else.
- Locally, `devdojo/`'s path repository moves from `../framework/laravel` to
  `../framework`. **Not done yet:** the platform still points at
  `../framework/laravel`, which keeps working.
- `laravel/composer.json` stays: the package's own tests run from it. Two
  files that say the same thing drift, so `composer_test.go` holds them
  together (same name, same requirements, same provider, the root's code
  found under `laravel/`) and fails for a new root folder that is neither
  kept for Composer nor marked `export-ignore`.

**Built.** `composer validate` passes, and an archive of the repo made the way
Composer makes one holds `composer.json`, `LICENSE`, `README.md`,
`laravel/src` and `laravel/config`.

This also removes the last thing between the platform and a deploy on
Framework that needed a new repository (the platform's `DEPLOY.md`): what is
left is the root `composer.json` and a pushed tag.

## 5. npm: `@devdojo/framework`

In an `npm/` folder beside `laravel/`. The package has a `bin` named
`devdojo` and no engine code of its own.

**How the binary arrives.** Two known ways:

- **(a) A package for each platform**, published by the release workflow
  (`@devdojo/framework-darwin-arm64`, `-linux-x64`, …), listed as
  `optionalDependencies` so npm installs only the one that fits. This is how
  esbuild and Biome ship. It needs no install script, so it works where
  scripts are turned off, and offline from a cache.
- **(b) A download on install**, from the GitHub release, checked against
  `checksums.txt`. One package to publish, but it needs the network and an
  install script, and some companies block both.

*Recommendation:* (a).

**Is downloading it wasteful?** No. The binary is about 5 MB, smaller than
most npm dependencies. With `npm install -g` it is installed **once** for the
machine and every site uses it; a site's folder holds only the site. A person
who adds it to one project instead (`npm install -D`) gets a copy in that
project's `node_modules`, which is what every build tool does, and npm's own
cache means it is fetched once per version.

## 6. The new commands

### `devdojo new <folder>`

**Built** (`cmd/devdojo/new.go`, package `starter`).

- Makes the folder and writes the starter site into it: the `src/` layout
  (`src/pages`, `src/components`, `src/data`, `src/css`), in the HTML syntax,
  with `public/` beside it. Then it prints the two commands to run next.
- The starter is **inside the binary** (`go:embed`, `starter/site/`), so `new`
  works offline and a version of the tool always writes a site that version
  can read. The test suite writes it and compiles every page, with no
  diagnostic and no notice allowed, so it cannot rot.
- It is a **copy** of `github.com/devdojo-templates/starter`, refreshed with
  `make starter STARTER=/path/to/starter`. Nothing under `starter/site/` is
  edited by hand. What only the template's listing needs stays behind:
  `template.json`, `thumbnail.png`, the template's README.
- It refuses a folder that exists and has anything in it, and leaves it as it
  was. An empty folder is fine.
- **Not built:** `--template <slug or git URL>`. Anything that looks like a
  flag is a usage error today, so a later `--template` is never taken for a
  folder name.
- **The cost:** the starter is 4.6 MB, most of it pictures, so the binary
  carries that. A build with no stripping went from about 5 MB to about
  15 MB with the starter and the web server `serve` needs. If that is too
  much, the pictures are the place to cut (smaller JPEGs in the template).

### `devdojo serve [dir]`

**Built** (`cmd/devdojo/serve.go`), with no new dependency: the reload is a
poll of the folder and an event stream, both from Go's standard library.

- Serves the site at `http://localhost:8000` (`--port` to change it; the next
  free port, said out loud, when 8000 is taken). It listens on this machine
  only, not on the network. `--no-reload` turns the reloading off.
- A page is compiled when it is asked for, by itself, so a big site starts at
  once. Dynamic pages, the 404 page and `public/**` are served the way a
  published site serves them.
- The browser reloads when a file changes.
- A page that does not compile shows what is wrong in the browser, with
  `file:line:col`, and in the terminal. The engine's notices (a site still in
  `resources/`) are printed in the terminal.
- CSS works with nothing installed: the engine already prints the Tailwind
  browser script for a site's stylesheet tag, which is right for development.
- Known gaps: a collection kept as a folder of files
  (`data/collections/<name>/<slug>.json`) is not matched by a dynamic page
  (the platform does not match one either), and the reload holds one
  connection for each open tab, so more than about six tabs of one site wait
  on each other.

### `devdojo build [dir]`

**Not built.**

- Writes the site as static files: clean URLs, one folder for each entry of a
  dynamic page, `public/**` copied, `robots.txt` and `sitemap.xml`.
- The platform's `Exporter` is the reference. `build` must write the same
  files for the same site, and the proof is the 79 catalog templates built
  both ways and compared.
- **CSS is the real work here.** A built site should ship a real stylesheet,
  not the browser script. The platform builds it with Tailwind's own
  standalone CLI; `build` would fetch that once, keep it beside the binary's
  cache, and fall back to the browser script with a warning when it cannot.

### What a site built this way does not get

On DevDojo, three things are added to a page after the engine has compiled
it: instant navigation's runtime, form wiring, and drawn forms. They live in
the platform, not here. A site built with `devdojo build` is the site as
written, without them. Moving instant navigation's runtime into this repo is
worth its own look, since it is plain JavaScript with no server half; forms
need a server and stay with DevDojo.

## 7. What this replaces

`@devdojo/sites` (`~/devdojo/sites`, command `site`: `site new`, `site dev`,
`site build`) is the Node CLI that exists today. It downloads Pocketknife
`0.2.0` and ports the platform's page mapping, dynamic pages and exporter to
JavaScript. Once `devdojo new`, `serve` and `build` exist it retires: its
last version prints where to go. That also takes it out of the work of moving
everything off Pocketknife (the platform plan's Phase 3 lists it as 23 files
to port).

## 8. Releases

One tag does everything:

1. goreleaser builds the binaries and the checksums (as now).
2. A new step publishes the npm packages at the tag's version.
3. Composer needs nothing: it reads the tag.

What only Tony can set up: an npm token for the `@devdojo` scope as a secret
of this repo, and the first push of a tag. The repo is public, so a tag is a
public release.

## 9. Open

1. **A template through `new`.** Convert to the HTML syntax and move to
   `src/` on the way in, or hand it over as it is (Blade, `resources/`) and
   let the notice say so? The first is kinder. With the catalog moving to
   `devdojo-templates`, where a template is already the site in `src/` and
   the HTML syntax, there may be nothing to convert.
2. **Which starter.** Answered: `devdojo-templates/starter`. A choice
   (`--starter blog`) is still open.
3. **`serve` for a Blade site.** The engine reads both syntaxes, so it works.
   Nothing to decide unless the starter should offer Blade too.
4. **Instant navigation in a built site** (§6).
5. **Homebrew.** A tap is a small addition to goreleaser. Wanted at launch?
6. **Which version.** `new` and `serve` are additions, so a minor version.
   The export design names 1.2.0 for export and 1.3.0 for `<Script>`; where
   this sits among them is a question of order, not of design.

## 10. Order, and where it stands

1. **The root `composer.json`** and `.gitattributes`. **Done.**
2. **`devdojo serve`.** **Done.**
3. **`devdojo new`** with the embedded starter. **Done.**
4. **The npm package** and the release step. Next. It needs an npm token for
   the `@devdojo` scope, which only Tony can make.
5. **`devdojo build`**, with the stylesheet step and the proof against the
   platform's exporter.
6. **Retire `@devdojo/sites`.**

None of it is released: there is no tag yet, so `devdojo new` and
`devdojo serve` exist only in a binary built from `main`.
