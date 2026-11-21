# One repo, three ways in: `devdojo new`, `devdojo serve`, npm and Composer

**Status:** Decided (Tony, 2026-11-20: "lock it in and build it"). Built the
same day: the root `composer.json` (§4), `devdojo new` (now downloading its
template from GitHub, `--template` included), `devdojo serve`, `devdojo build`
(§6) and the npm package with its release step (§5). Nothing is released yet:
there is no tag. "Where it stands" at the end has the list.
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
   template DevDojo lists first. `devdojo new` downloads it from GitHub; it
   is not kept inside the binary (Tony, later the same day: the starter
   lives outside the binary) (§6).

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
  dist/                what `devdojo build` writes
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

> **Renamed the same day: the package is `devdojo`** (`npm install -g
> devdojo`), Tony's own unscoped name, and the platform packages are
> `devdojo-<os>-<cpu>`; the folder is `npm/devdojo`. Read the names below
> that way. `devdojo` 1.0.11 was an older CLI whose only command,
> `devdojo verify <key>`, devdojo.com's dashboard still tells new authors to
> run, so the package answers that one command itself
> (`npm/devdojo/lib/verify.js`, tested by `npm/test/`) and hands everything
> else to the binary. Framework 1.1.0 is newer than 1.0.11, so it is a plain
> upgrade for anyone who had the old one.

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

*Recommendation:* (a). **Built that way** (`npm/`):

- `npm/framework` is `@devdojo/framework`: a `bin` named `devdojo` that is a
  short Node script. It finds `@devdojo/framework-<os>-<cpu>` (darwin-arm64,
  darwin-x64, linux-arm64, linux-x64, win32-x64), runs the binary in it with
  the same arguments, and exits with its exit code. Without the platform
  package (an install with optional dependencies turned off) it says that,
  in a sentence, and how to fix it.
- `npm/scripts/packages.mjs --version X.Y.Z --dist dist --out npm/out` writes
  all six packages for a version from goreleaser's `dist/` (it finds the
  binaries through `dist/artifacts.json`); `--binary darwin-arm64=<path>`
  names one by hand, to try it locally.
- The release workflow runs that after goreleaser and publishes the five
  platform packages, then the main one, with npm provenance. It only does
  so when the repository has an `NPM_TOKEN` secret, and skips npm otherwise.
- Proved without publishing: `npm pack` of the main package and the
  darwin-arm64 one (with a binary built here), both installed into an empty
  prefix with `--offline`, and `devdojo version` and `devdojo lint` ran
  through the installed command. With only the main package installed the
  command prints the sentence above and exits 1.
- The binary still says the version in `blade.Version`, not the tag's: a
  release must keep the two the same (or goreleaser could set it).

**Is downloading it wasteful?** No. The binary is about 5 MB, smaller than
most npm dependencies. With `npm install -g` it is installed **once** for the
machine and every site uses it; a site's folder holds only the site. A person
who adds it to one project instead (`npm install -D`) gets a copy in that
project's `node_modules`, which is what every build tool does, and npm's own
cache means it is fetched once per version.

## 6. The new commands

### `devdojo new <folder> [--template <name>]`

**Built** (`cmd/devdojo/new.go`, package `starter`).

- Downloads a template from GitHub and writes it into the folder: by
  default the starter, `github.com/devdojo-templates/starter`, which is the
  `src/` layout in the HTML syntax with `public/` beside it. Then it prints
  the two commands to run next.
- `--template` names another: a bare name is a template of
  `devdojo-templates` (`--template blog`), or `owner/repo`, or a repository's
  `https://github.com/owner/repo` address. GitHub only, for now.
- The download is the default branch as GitHub packs it
  (`https://codeload.github.com/<owner>/<repo>/tar.gz/HEAD`). It is unpacked
  into a folder of its own beside the target, checked, and only then moved
  into place. What only the template's listing needs stays behind:
  `template.json`, `thumbnail.png` and `README.md` at the top, `.DS_Store`
  anywhere, and the repository's `.git*` files except `.gitignore` (and
  `.gitkeep`, which only holds a folder open).
- It does not trust the download: a name that leaves the folder, a link of
  either kind or a device stops it, it stops past 200 MB or 50,000 files,
  and a download with no `src/` or `resources/` folder is "not a site".
- It refuses a folder that exists and has anything in it, before it
  downloads anything, and leaves it as it was. An empty folder is fine. Any
  failure after that leaves nothing on disk, not even the folder.
- What it says when something goes wrong: "couldn't reach GitHub…" with no
  connection, "there is no template called X at github.com/…" for a 404, and
  "… is not a site" for a repository that is not one.
- `DEVDOJO_TEMPLATES_URL` points the download somewhere else, which is how
  the tests run against a small site served on this machine and never need
  the network. One test downloads the real starter and compiles it, when
  `DEVDOJO_NETWORK_TESTS=1`.
- **Why not inside the binary any more:** Tony asked for the starter to live
  outside it. The binary dropped from about 15 MB to about 11.8 MB with
  `build` in it (a plain `go build`; 8.2 MB stripped with `-s -w`, as a
  release is). A new site now needs the network. The price is
  that a version of the tool can be handed a template newer than it reads;
  `devdojo lint` right after `new` says so if it ever happens.

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

### `devdojo build [dir] [--out dist] [--url https://example.com]`

**Built** (`cmd/devdojo/build.go`, `tailwind.go`, and `pages.go`, which
holds the routing rules `serve` and `build` share).

- Writes the site as static files into `dist/` (or `--out`): every page at a
  clean address (`pricing.html` → `pricing/index.html`; the home page, a
  folder's `index` page and the 404 page keep their names, so the 404 page is
  `404.html`), a dynamic page once for each entry of its collection
  (`blog/[posts.slug].html` → `blog/<slug>/index.html`), `public/**` copied
  as it is, `robots.txt`, and `sitemap.xml`.
- Which page is written where is asked of the same function `serve` answers
  with: a page is written only at the address it serves. So a page with a
  Blade twin, a folder's index page beside a flat page of the same name, or a
  dynamic entry whose address a static page already has, is left out exactly
  as `serve` leaves it out, and `build` says so.
- Nothing is written while any page has a diagnostic of any kind. They are
  printed as `serve` prints them (`file:line:col  message`, a layout's
  mistake once, not once per page), then "Nothing was built". Notices are
  printed once each. A folder with no pages is refused too.
- The sitemap needs the site's address: `--url`, or a `url` in
  `src/data/site.json`. Without one there is no sitemap, `robots.txt` has no
  `Sitemap:` line, and `build` says how to give one. A site's own
  `public/robots.txt` or `public/sitemap.xml` is the one shipped.
- **The stylesheet.** The pages' stylesheet sources are read back out of
  the built pages (what `<Vite>` printed), joined, given
  `@import "tailwindcss"` when they have no `@import` (as the browser script
  does), and built by Tailwind's standalone CLI with every built page and
  every `.html`, `.blade.php`, `.js`, `.json` and `.md` file of the site's
  source and `public/` to look in. The result is
  `assets/site.<hash>.css`, linked where the browser script was. Tailwind is
  v4.3.3, the platform's, downloaded the first time from Tailwind's GitHub
  release into `os.UserCacheDir()/devdojo/`, and kept only when its SHA-256
  matches the release's (the sums are in the code). When it cannot be had
  (no connection, a mismatch, no build for the machine), the pages keep the
  browser script and one warning says so. When Tailwind runs and fails (a
  mistake in the CSS), the build stops with Tailwind's message: unlike a
  publish on DevDojo, nobody loses anything by being told.
  `DEVDOJO_TAILWIND` names a CLI to use instead (the tests give a stand-in),
  `DEVDOJO_TAILWIND_URL` moves the download.
- A new build is put together beside `dist/` and swapped in whole. A folder
  inside the site is replaced; one outside it that has something in it is
  refused, never emptied. The site's own folders (`src/`, `resources/`,
  `public/`, the site itself, a folder above it) are refused as `--out`.
- **Proved on the catalog:** the 79 templates in
  `devdojo/resources/sites-templates/` (copied first, never built in place)
  all build with no diagnostic: 936 pages, 809 files from `public/`, and a
  built stylesheet for every one. The 78 still in `resources/` print the
  notice that says so. The starter from GitHub builds 18 pages.
- **What differs from the platform's `Exporter`** besides the three things
  below: no `llms.txt`; no `lastmod` in the sitemap (a file's time on disk
  says when it was copied, not when the page changed); no `noindex` for
  pages a site hides from search engines (that list lives on DevDojo); a
  Tailwind failure stops the build instead of falling back; and a flat page
  beside a folder's index page of the same name is decided the way `serve`
  decides it (the flat one), where the exporter keeps whichever it writes
  last. A collection kept as a folder of files is not fanned out by a
  dynamic page, the same gap `serve` and the platform have.

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

1. **A template through `new`.** Answered for templates in the new shape:
   `--template` downloads any GitHub repository that holds a site and writes
   it as it is. A template still in Blade and `resources/` is written as it
   is too, and the notice says so; converting it on the way in is not built.
2. **Which starter.** Answered: `devdojo-templates/starter`, and
   `--template` picks another.
3. **`serve` for a Blade site.** The engine reads both syntaxes, so it works.
   Nothing to decide unless the starter should offer Blade too.
4. **Instant navigation in a built site** (§6).
5. **Homebrew.** A tap is a small addition to goreleaser. Wanted at launch?
6. **Which version.** `new`, `serve` and `build` are additions, so a minor
   version. The export design names 1.2.0 for export and 1.3.0 for
   `<Script>`; where this sits among them is a question of order, not of
   design. `blade.Version` must match the tag (§5).
7. **`new` with no network.** It now needs one. A cached copy of the last
   download is the obvious answer if that turns out to matter.

## 10. Order, and where it stands

1. **The root `composer.json`** and `.gitattributes`. **Done.**
2. **`devdojo serve`.** **Done.**
3. **`devdojo new`**, downloading the starter or `--template` from GitHub.
   **Done.**
4. **The npm package** and the release step. **Done**, not published: it
   waits for an npm token for the `@devdojo` scope as the repository's
   `NPM_TOKEN` secret, which only Tony can make, and a pushed tag.
5. **`devdojo build`**, with the stylesheet step, proved on the catalog.
   **Done.** Not done: a byte-for-byte comparison with the platform's
   exporter on the same sites (the differences in kind are listed in §6).
6. **Retire `@devdojo/sites`.** Next, once a release is out.

None of it is released: there is no tag, so all of it exists only in a
binary built from `main`.
