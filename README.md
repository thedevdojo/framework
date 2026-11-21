# framework

<img src="https://cdn.devdojo.com/images/november2026/framewrk.png" style="height:auto; width:100%">

**DevDojo Framework turns plain HTML into a finished website.** You write pages in HTML with a few extra tags, and the `devdojo` command builds them into static files you can host anywhere. It's one small program written in Go: no PHP, no server, nothing running in production.

## Install

Build it from source with [Go](https://go.dev/dl/) (1.22 or newer):

```bash
go install github.com/thedevdojo/framework/cmd/devdojo@latest
```

That puts `devdojo` in Go's bin folder, `$(go env GOPATH)/bin`. Make sure that folder is on your `PATH`, then check it worked:

```bash
devdojo version
```

Once the first release is out, you'll also be able to install it with npm (`npm install -g @devdojo/framework`) or download a ready-made binary from [Releases](https://github.com/thedevdojo/framework/releases).

## Start a site

```bash
devdojo new my-site
cd my-site
devdojo serve
```

`new` downloads the [starter template](https://github.com/devdojo-templates/starter) into `my-site/`. `serve` runs it at **http://localhost:8000** and reloads the browser every time you save a file. If a page has a mistake, the browser shows you the file, line and what's wrong.

Want a different starting point? `devdojo new my-site --template <name>` takes a template from [devdojo-templates](https://github.com/devdojo-templates), an `owner/repo`, or any GitHub repository's address.

## What's in a site

```
my-site/
├── public/            # images, fonts, scripts: copied as they are
└── src/
    ├── pages/         # one file per page: about.html is /about
    ├── components/    # reusable pieces, plus layouts/ for page shells
    ├── data/          # site.json and collections/*.json
    └── css/           # your stylesheet
```

## Write a page

A page is HTML. Values from your data go in single braces, and tags that start with a capital letter are the template's own. Here's a whole small site.

The data, in `src/data/site.json` and `src/data/collections/posts.json`:

```json
{ "name": "My site" }
```

```json
[
    { "slug": "hello-world", "title": "Hello world", "featured": true },
    { "slug": "second-post", "title": "Second post", "featured": false }
]
```

A layout every page sits in, `src/components/layouts/main.html`. `<Props>` lists what it accepts, with defaults, and `<Slot />` is where the page goes:

```html
<Props title="Home" />
<!doctype html>
<html>
<head>
    <title>{title} · {site.name}</title>
</head>
<body>
    <Slot />
</body>
</html>
```

A component, `src/components/card.html`. Its file name is its tag, so this one is `<Card>`:

```html
<Props title="" />
<div class="card">
    <h2>{title}</h2>
    <Slot />
</div>
```

The homepage, `src/pages/index.html`:

```html
<Layout src="main.html" title="Home">
    <h1>Welcome to {site.name}</h1>

    <Foreach posts="post">
        <Card :title="post.title">
            <If post.featured>
                <p>Featured</p>
            </If>
            <a href="/blog/{post.slug}">Read it</a>
        </Card>
    </Foreach>
</Layout>
```

`<Foreach>` repeats once for each post, `<If>` shows its insides only when the condition is true, and `:title` hands a value from your data to the component.

One page for every post, `src/pages/blog/[posts.slug].html`. The brackets in the file name make a page for each entry in `posts`, at `/blog/hello-world` and `/blog/second-post`, and `posts` is that one post inside:

```html
<Layout src="main.html" :title="posts.title">
    <h1>{posts.title}</h1>
</Layout>
```

The cheat sheet:

| You write | It means |
|---|---|
| `{post.title}` | print a value |
| `<If post.featured>` … `<Else>` … `</If>` | show something only sometimes |
| `<If condition="plan == 'pro'">` | the same, with a comparison |
| `<Foreach posts="post">` … `</Foreach>` | repeat for each item |
| `<Layout src="main.html">` | wrap the page in a layout |
| `<Card>`, `<SectionsHero />` | use a component (`components/sections/hero.html` is `<SectionsHero />`) |
| `\{title}` | print a real brace |

Plain HTML is lowercase and never touched, so Alpine's `x-data="{ open: false }"` and `@click`, and anything in `<script>` or `<style>`, just work. Every tag and rule is in [the HTML syntax guide](docs/html-syntax.md).

## Build it

```bash
devdojo build
```

That writes the finished site to `dist/`: every page at a clean address (`about.html` becomes `about/index.html`), one page per post, `public/` copied over, and a `robots.txt`. Upload `dist/` to any static host (Netlify, Cloudflare Pages, GitHub Pages, an S3 bucket, your own server).

Give it your site's address and you get a `sitemap.xml` too:

```bash
devdojo build --url https://example.com
```

Nothing gets written if any page has a problem. You get a list of `file:line:col  what's wrong` instead.

## A few more commands

```bash
devdojo lint .
```

Checks every page without building. No output means all good.

```bash
devdojo convert . --to blade
```

Rewrites the site in [Blade](docs/blade-syntax.md), the syntax Laravel uses (and `--to html` goes back). Framework reads both, and every page has to come out exactly the same or nothing changes.

```bash
devdojo migrate .
```

Moves an older site (one that keeps its files in `resources/`, or writes `{{ x }}`) to today's `src/` folder and single braces.

## Learn more

- [The HTML syntax](docs/html-syntax.md): every tag, shorthand and rule.
- [The Blade syntax](docs/blade-syntax.md): the same language, written the way Laravel writes it.
- [In detail](docs/reference.md): why it's built this way, data, the folders a site may use, and the JSON protocol for running the engine from your own program.

## License

MIT. See [LICENSE](LICENSE).
