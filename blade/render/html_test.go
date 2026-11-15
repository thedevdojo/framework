package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/vfs"
)

const (
	htmlEntry  = "resources/views/pages/index.html"
	bladeEntry = "resources/views/pages/index.blade.php"
)

// compileHTML renders pages/index.html from an in-memory site.
func compileHTML(t *testing.T, files map[string]string) Result {
	t.Helper()
	return Compile(vfs.Mem(files), []string{htmlEntry}, Options{})
}

// htmlPage renders one .html page and returns its output and diagnostics.
func htmlPage(t *testing.T, files map[string]string) (string, []Diagnostic) {
	t.Helper()
	res := compileHTML(t, files)
	return res.Files["index.html"], res.Diagnostics
}

// requireClean fails when a render produced any diagnostic.
func requireClean(t *testing.T, diags []Diagnostic) {
	t.Helper()
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
}

// TestHTMLRendersLikeBlade is the promise of the HTML syntax: a site written
// with tags renders the very same bytes as the same site written in Blade.
// Each case is one site, twice.
func TestHTMLRendersLikeBlade(t *testing.T) {
	const views = "resources/views/"
	cases := []struct {
		name  string
		data  map[string]string // shared by both: resources/data/**, css
		html  map[string]string // resources/views/** in the HTML syntax
		blade map[string]string // resources/views/** in Blade
	}{
		{
			name: "conditions",
			data: map[string]string{"resources/data/site.json": `{"plan": "team", "beta": false}`},
			html: map[string]string{"pages/index.html": `<If condition="site.plan == 'pro'">
  <p>Pro</p>
<ElseIf condition="site.plan = 'team'">
  <p>Team</p>
<Else>
  <p>Free</p>
</If>
<If site.beta><b>beta</b></If>
<If not="site.beta"><i>stable</i></If>
<If site.plan="team"><u>team</u></If>
`},
			blade: map[string]string{"pages/index.blade.php": `@if($site->plan == 'pro')
  <p>Pro</p>
@elseif($site->plan == 'team')
  <p>Team</p>
@else
  <p>Free</p>
@endif
@if($site->beta)<b>beta</b>@endif
@if(!($site->beta))<i>stable</i>@endif
@if($site->plan == 'team')<u>team</u>@endif
`},
		},
		{
			name: "loops",
			data: map[string]string{
				"resources/data/site.json":              `{"name": "Acme", "tag": "x"}`,
				"resources/data/collections/posts.json": `[{"title": "A", "draft": false}, {"title": "B", "draft": true}, {"title": "C", "draft": false}, {"title": "D", "draft": false}]`,
			},
			html: map[string]string{"pages/index.html": `<ul>
<Foreach posts="post">
  <li>{{ loop.iteration }}/{{ loop.count }} {{ post.title }}<If loop.last> (last)</If></li>
</Foreach>
</ul>
<Foreach site="key, value">{{ key }}={{ value }};</Foreach>
<Foreach posts="post"><Continue condition="post.draft" />{{ post.title }}<Break condition="loop.iteration == 3" /></Foreach>
<ForEach collection="posts" as="p">{{ p.title }}</ForEach>
`},
			blade: map[string]string{"pages/index.blade.php": `<ul>
@foreach($posts as $post)
  <li>{{ $loop->iteration }}/{{ $loop->count }} {{ $post->title }}@if($loop->last) (last)@endif</li>
@endforeach
</ul>
@foreach($site as $key => $value){{ $key }}={{ $value }};@endforeach
@foreach($posts as $post)@continue($post->draft){{ $post->title }}@break($loop->iteration == 3)@endforeach
@foreach($posts as $p){{ $p->title }}@endforeach
`},
		},
		{
			name: "layout, components, props, slots and the attributes bag",
			data: map[string]string{"resources/data/site.json": `{"cta": "Start <now>"}`},
			html: map[string]string{
				"pages/index.html": `<Layout src="main.html" title="Home">
<Slot name="head"><meta name="x"></Slot>
<SectionsHero badge="Personal Blog" :cta="site.cta" />
<Button href="/go" class="big">Go <b>now</b></Button>
<Include src="button.html" />
</Layout>
`,
				"components/layouts/main.html": `<Props title="Untitled" />
<html><head><title>{{ title }}</title>{{ head ?? '' }}</head>
<body>
{{ slot }}
</body></html>
`,
				"components/sections/hero.html": `<Props badge="Badge" heading="Hello" cta />
<section><span>{{ badge }}</span><h1>{{ heading }}</h1><a>{{ cta }}</a></section>
`,
				"components/button.html": `<Props href="#" />
<a href="{{ href }}" {{ attributes.merge(['class' => 'btn']) }}>{{ slot }}</a>
`,
			},
			blade: map[string]string{
				"pages/index.blade.php": `<x-layouts.main title="Home">
<x-slot:head><meta name="x"></x-slot>
<x-sections.hero badge="Personal Blog" :cta="$site->cta" />
<x-button href="/go" class="big">Go <b>now</b></x-button>
<x-button />
</x-layouts.main>
`,
				"components/layouts/main.blade.php": `@props(['title' => 'Untitled'])
<html><head><title>{{ $title }}</title>{{ $head ?? '' }}</head>
<body>
{{ $slot }}
</body></html>
`,
				"components/sections/hero.blade.php": `@props(['badge' => 'Badge', 'heading' => 'Hello', 'cta'])
<section><span>{{ $badge }}</span><h1>{{ $heading }}</h1><a>{{ $cta }}</a></section>
`,
				"components/button.blade.php": `@props(['href' => '#'])
<a href="{{ $href }}" {{ $attributes->merge(['class' => 'btn']) }}>{{ $slot }}</a>
`,
			},
		},
		{
			name: "slot tags print slots",
			html: map[string]string{
				"pages/index.html":       `<Card><Slot name="title">T</Slot>body</Card><Card>only body</Card>`,
				"components/card.html":   `<div><h2><Slot name="title" /></h2><Slot /></div>`,
				"components/unused.html": ``,
			},
			blade: map[string]string{
				"pages/index.blade.php":     `<x-card><x-slot:title>T</x-slot>body</x-card><x-card>only body</x-card>`,
				"components/card.blade.php": `<div><h2>{{ $title ?? '' }}</h2>{{ $slot }}</div>`,
			},
		},
		{
			name: "output forms and assets",
			data: map[string]string{
				"resources/data/site.json": `{"html": "<b>&</b>"}`,
				"resources/css/site.css":   "@import \"tailwindcss\";\n",
			},
			html: map[string]string{"pages/index.html": `<head>
<Vite src="resources/css/site.css" />
</head>
{{ site.html }}|{!! site.html !!}|{{-- gone --}}|@{{ kept }}|{{ site.missing ?? 'fallback' }}
`},
			blade: map[string]string{"pages/index.blade.php": `<head>
@vite(['resources/css/site.css'])
</head>
{{ $site->html }}|{!! $site->html !!}|{{-- gone --}}|@{{ kept }}|{{ $site->missing ?? 'fallback' }}
`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			build := func(viewFiles map[string]string) vfs.Mem {
				fs := vfs.Mem{}
				for k, v := range tc.data {
					fs[k] = v
				}
				for k, v := range viewFiles {
					fs[views+k] = v
				}
				return fs
			}
			html := Compile(build(tc.html), []string{htmlEntry}, Options{})
			blade := Compile(build(tc.blade), []string{bladeEntry}, Options{})
			requireClean(t, blade.Diagnostics)
			requireClean(t, html.Diagnostics)
			got, want := html.Files["index.html"], blade.Files["index.html"]
			if want == "" {
				t.Fatal("the Blade twin rendered nothing — the case is broken")
			}
			if got != want {
				t.Errorf("HTML and Blade disagree\n--- html ---\n%s\n--- blade ---\n%s", got, want)
			}
		})
	}
}

// TestHTMLPageOutputName: pages/about.html renders to about.html, the same
// output name pages/about.blade.php has.
func TestHTMLPageOutputName(t *testing.T) {
	cases := map[string]string{
		"resources/views/pages/index.html":            "index.html",
		"resources/views/pages/about.html":            "about.html",
		"resources/views/pages/blog/index.html":       "blog/index.html",
		"resources/views/pages/index.blade.php":       "index.html",
		"resources/views/pages/docs/intro.blade.php":  "docs/intro.html",
		"resources/views/partials/thing.html":         "partials/thing.html",
		"resources/views/pages/post/[post.slug].html": "post/[post.slug].html",
	}
	for entry, want := range cases {
		if got := outputName(entry); got != want {
			t.Errorf("outputName(%q) = %q; want %q", entry, got, want)
		}
	}
}

// TestHTMLComponentResolution: a component tag finds its file by trying a
// folder, then a dash, at each capital letter — .html first, then the Blade
// file of the same name, so an HTML page can use an existing Blade component.
func TestHTMLComponentResolution(t *testing.T) {
	const comps = "resources/views/components/"
	cases := []struct {
		name  string
		files map[string]string
		want  string
		deps  []string
	}{
		{
			name:  "a folder",
			files: map[string]string{comps + "sections/hero.html": "folder"},
			want:  "folder",
			deps:  []string{htmlEntry, comps + "sections/hero.html"},
		},
		{
			name:  "a dash",
			files: map[string]string{comps + "sections-hero.html": "dash"},
			want:  "dash",
			deps: []string{htmlEntry,
				comps + "sections/hero.html", comps + "sections/hero.blade.php",
				comps + "sections-hero.html"},
		},
		{
			name: "the folder wins over the dash",
			files: map[string]string{
				comps + "sections/hero.html": "folder",
				comps + "sections-hero.html": "dash",
			},
			want: "folder",
			deps: []string{htmlEntry, comps + "sections/hero.html"},
		},
		{
			name:  "a Blade component",
			files: map[string]string{comps + "sections/hero.blade.php": "blade {{ $site->name ?? 'x' }}"},
			want:  "blade x",
			deps:  []string{htmlEntry, comps + "sections/hero.html", comps + "sections/hero.blade.php"},
		},
		{
			name: ".html wins over .blade.php of the same name",
			files: map[string]string{
				comps + "sections/hero.html":      "html",
				comps + "sections/hero.blade.php": "blade",
			},
			want: "html",
			deps: []string{htmlEntry, comps + "sections/hero.html"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{htmlEntry: `<SectionsHero />`}
			for k, v := range tc.files {
				files[k] = v
			}
			res := compileHTML(t, files)
			requireClean(t, res.Diagnostics)
			if got := res.Files["index.html"]; got != tc.want {
				t.Errorf("output = %q; want %q", got, tc.want)
			}
			if got := res.Deps["index.html"]; !reflect.DeepEqual(got, tc.deps) {
				t.Errorf("deps = %v\n want  %v", got, tc.deps)
			}
		})
	}
}

// TestHTMLComponentMissing: an unknown component tag leaves the stable marker
// and one diagnostic that says where the engine looked. Every path it looked
// for joins deps, so creating any of them re-renders the page.
func TestHTMLComponentMissing(t *testing.T) {
	const comps = "resources/views/components/"
	res := compileHTML(t, map[string]string{htmlEntry: `X<SectionsHero />Y`})
	if got, want := res.Files["index.html"], `X<!-- pocketknife:missing component "sections.hero" -->Y`; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v; want one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	wantMsg := `component <SectionsHero> not found — looked for ` + comps + `sections/hero.html, ` + comps + `sections-hero.html`
	if d.Kind != KindError || d.Construct != "component-missing" || d.Message != wantMsg || d.Line != 1 || d.Col != 2 {
		t.Errorf("diagnostic = %+v\n want message %q", d, wantMsg)
	}
	wantDeps := []string{htmlEntry,
		comps + "sections/hero.html", comps + "sections/hero.blade.php",
		comps + "sections-hero.html", comps + "sections-hero.blade.php"}
	if got := res.Deps["index.html"]; !reflect.DeepEqual(got, wantDeps) {
		t.Errorf("deps = %v\n want  %v", got, wantDeps)
	}

	// A missing layout names the file the src asked for.
	res = compileHTML(t, map[string]string{htmlEntry: `<Layout src="main.html">x</Layout>`})
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v; want one", res.Diagnostics)
	}
	if got, want := res.Diagnostics[0].Message, `component <Layout> not found — looked for `+comps+`layouts/main.html`; got != want {
		t.Errorf("message = %q; want %q", got, want)
	}
}

// TestBladeTagsStayBlade: a Blade <x-tag> names exactly one .blade.php file,
// as it does in real Laravel — it never falls back to an .html component, and
// its deps are unchanged by the HTML syntax existing.
func TestBladeTagsStayBlade(t *testing.T) {
	res := Compile(vfs.Mem{
		bladeEntry:                               `<x-button />`,
		"resources/views/components/button.html": "html button",
	}, []string{bladeEntry}, Options{})
	if got, want := res.Files["index.html"], `<!-- pocketknife:missing component "button" -->`; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
	wantDeps := []string{bladeEntry, "resources/views/components/button.blade.php"}
	if got := res.Deps["index.html"]; !reflect.DeepEqual(got, wantDeps) {
		t.Errorf("deps = %v; want %v", got, wantDeps)
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Message != `component "button" not found` {
		t.Errorf("diagnostics = %+v", res.Diagnostics)
	}
}

// TestHTMLPageUsesBladeLayout: the two syntaxes mix file by file — an .html
// page inside a Blade layout, with a Blade component nested in it.
func TestHTMLPageUsesBladeLayout(t *testing.T) {
	out, diags := htmlPage(t, map[string]string{
		htmlEntry: `<Layout src="main" title="Mixed"><Badge label="new" /></Layout>`,
		"resources/views/components/layouts/main.blade.php": "@props(['title' => 'x'])\n<title>{{ $title }}</title>{{ $slot }}",
		"resources/views/components/badge.blade.php":        "@props(['label'])\n<i>{{ $label }}</i>",
	})
	requireClean(t, diags)
	if want := `<title>Mixed</title><i>new</i>`; out != want {
		t.Errorf("output = %q; want %q", out, want)
	}
}

// TestHTMLForeachLimit: limit cuts the list before the loop runs, so
// loop.count and loop.last describe what is shown.
func TestHTMLForeachLimit(t *testing.T) {
	site := map[string]string{
		"resources/data/site.json":              `{"per_page": 2}`,
		"resources/data/collections/posts.json": `[{"t": "A"}, {"t": "B"}, {"t": "C"}, {"t": "D"}]`,
	}
	cases := []struct{ name, page, want string }{
		{"a number", `<Foreach posts="post" limit="3">{{ post.t }}{{ loop.iteration }}/{{ loop.count }}<If loop.last>!</If> </Foreach>`, `A1/3 B2/3 C3/3! `},
		{"an expression", `<Foreach posts="post" limit="site.per_page">{{ post.t }}</Foreach>`, `AB`},
		{"more than there are", `<Foreach posts="post" limit="10">{{ post.t }}</Foreach>`, `ABCD`},
		{"zero", `[<Foreach posts="post" limit="0">{{ post.t }}</Foreach>]`, `[]`},
		{"Static's count", `<ForEach collection="posts" as="post" count="1">{{ post.t }}</ForEach>`, `A`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{htmlEntry: tc.page}
			for k, v := range site {
				files[k] = v
			}
			out, diags := htmlPage(t, files)
			requireClean(t, diags)
			if out != tc.want {
				t.Errorf("output = %q; want %q", out, tc.want)
			}
		})
	}

	// A limit that is not a whole number is an error, and the loop runs in full.
	files := map[string]string{htmlEntry: `<Foreach posts="post" limit="'many'">{{ post.t }}</Foreach>`}
	for k, v := range site {
		files[k] = v
	}
	out, diags := htmlPage(t, files)
	if out != "ABCD" {
		t.Errorf("output = %q; want the full loop", out)
	}
	if len(diags) != 1 || diags[0].Kind != KindError || diags[0].Construct != "foreach" ||
		diags[0].Message != "limit must be a whole number, zero or more" {
		t.Errorf("diagnostics = %+v", diags)
	}
}

// TestHTMLAttributeInterpolation: a plain attribute on a component tag may
// embed {{ }}. The component receives the joined text as an ordinary string
// prop (escaped when it is printed, like any other), and the same text lands
// in the attributes bag with the embedded values escaped.
func TestHTMLAttributeInterpolation(t *testing.T) {
	out, diags := htmlPage(t, map[string]string{
		"resources/data/site.json": `{"slug": "a&b", "n": 3}`,
		htmlEntry:                  `<Link href="/posts/{{ site.slug }}" label="{{ site.n }} items" data-id="p-{{ site.slug }}" />`,
		"resources/views/components/link.html": `<Props href label />
<a href="{{ href }}" {{ attributes }}>{{ label }}</a>`,
	})
	requireClean(t, diags)
	if want := `<a href="/posts/a&amp;b" data-id="p-a&amp;b">3 items</a>`; out != want {
		t.Errorf("output = %q\n want    %q", out, want)
	}

	// The text around a value is text too: what lands in the attributes bag is
	// escaped as a whole, so a quote or an ampersand in it cannot break the tag.
	out, diags = htmlPage(t, map[string]string{
		"resources/data/site.json":             `{"q": "x y", "n": 3, "on": true}`,
		htmlEntry:                              `<Link data-u='/s?a=1&q={{ site.q }}' data-t='say "{{ site.n }}"' data-b="{{ site.on }}" data-r="{!! site.q !!}" />`,
		"resources/views/components/link.html": `<a {{ attributes }}></a>`,
	})
	requireClean(t, diags)
	if want := `<a data-u="/s?a=1&amp;q=x y" data-t="say &quot;3&quot;" data-b="1" data-r="x y"></a>`; out != want {
		t.Errorf("output = %q\n want    %q", out, want)
	}

	// An expression that cannot be evaluated is reported at the attribute.
	_, diags = htmlPage(t, map[string]string{
		htmlEntry:                              "<Link\n  href=\"/{{ nope }}\" />",
		"resources/views/components/link.html": `<Props href="#" />{{ href }}`,
	})
	if len(diags) != 1 || diags[0].Construct != "undefined-variable" || diags[0].Line != 2 || diags[0].Col != 3 {
		t.Errorf("diagnostics = %+v; want one undefined-variable at 2:3", diags)
	}
}

// TestHTMLDiagnosticsSpeakHTML: messages for an .html file never show Blade
// spellings — no "$" on a variable, no "@props".
func TestHTMLDiagnosticsSpeakHTML(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		construct string
		message   string
	}{
		{
			name:      "undefined variable",
			files:     map[string]string{htmlEntry: `{{ titel }}`},
			construct: "undefined-variable",
			message:   "undefined variable titel",
		},
		{
			name: "undefined property",
			files: map[string]string{
				htmlEntry:                  `{{ site.titel }}`,
				"resources/data/site.json": `{"title": "x"}`,
			},
			construct: "undefined-property",
			message:   "undefined property titel",
		},
		{
			name: "props not first",
			files: map[string]string{
				htmlEntry:                              `<Card />`,
				"resources/views/components/card.html": `<div></div><Props title="x" />`,
			},
			construct: "props",
			message:   "<Props> is only valid as the first thing in a component file",
		},
		{
			name: "props default that is not a plain value",
			files: map[string]string{
				htmlEntry:                              `<Card />`,
				"resources/views/components/card.html": `<Props :count="1 + 2" />x`,
			},
			construct: "props",
			message:   `<Props> has a default that is not a plain value — use text, or a number, true, false, null or a list after a colon (:count="3")`,
		},
		{
			name: "break inside a component, outside its loop",
			files: map[string]string{
				htmlEntry:                              `<Foreach site="v"><Card><Break /></Card></Foreach>`,
				"resources/data/site.json":             `{"a": 1}`,
				"resources/views/components/card.html": `{{ slot }}`,
			},
			construct: "loop-jump",
			message:   "<Break> and <Continue> only work directly inside a <Foreach>",
		},
		{
			name:      "vite asset missing",
			files:     map[string]string{htmlEntry: `<Vite src="resources/css/gone.css" />`},
			construct: "<Vite>",
			message:   `vite asset "resources/css/gone.css" not found`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := htmlPage(t, tc.files)
			if len(diags) != 1 {
				t.Fatalf("diagnostics = %+v; want exactly one", diags)
			}
			if diags[0].Construct != tc.construct || diags[0].Message != tc.message {
				t.Errorf("diagnostic = %q %q\n want       %q %q", diags[0].Construct, diags[0].Message, tc.construct, tc.message)
			}
			if strings.Contains(diags[0].Message, "$") || strings.Contains(diags[0].Message, "@") {
				t.Errorf("message leaks Blade spelling: %q", diags[0].Message)
			}
		})
	}
}

// TestHTMLHasNoDirectives: in an .html file "@" is just a character. Alpine
// handlers, CSS at-rules and Blade directives all pass through as written and
// none of them is reported.
func TestHTMLHasNoDirectives(t *testing.T) {
	page := `<button @click="open = true">x</button><style>@media (min-width: 1px) { a { color: red } }</style>@if(true) @auth @php hello@example.com`
	out, diags := htmlPage(t, map[string]string{htmlEntry: page})
	requireClean(t, diags)
	if out != page {
		t.Errorf("output = %q; want the page back untouched", out)
	}
}

// TestHTMLCommentsAreOpaque: an HTML comment is emitted as written and nothing
// inside it runs, so commenting a block out switches it off.
func TestHTMLCommentsAreOpaque(t *testing.T) {
	page := `a<!-- <Missing /> {{ nope }} <If condition="x">y</If> -->b`
	out, diags := htmlPage(t, map[string]string{htmlEntry: page})
	requireClean(t, diags)
	if out != page {
		t.Errorf("output = %q; want the page back untouched", out)
	}
}

// TestPageCollision: pages/index.html and pages/index.blade.php both render to
// index.html. Rendering both in one compile is reported instead of letting one
// silently replace the other.
func TestPageCollision(t *testing.T) {
	res := Compile(vfs.Mem{
		bladeEntry: "blade",
		htmlEntry:  "html",
	}, []string{bladeEntry, htmlEntry}, Options{})
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v; want one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	want := `two pages render to index.html: ` + bladeEntry + ` and ` + htmlEntry + ` — keep one`
	if d.Kind != KindError || d.Construct != "page-collision" || d.File != htmlEntry || d.Message != want {
		t.Errorf("diagnostic = %+v\n want message %q", d, want)
	}

	// One entry rendered to several outputs (a dynamic page's variants) is not
	// a collision, and neither is an explicit output replacing an earlier one.
	res = CompileVariants(vfs.Mem{htmlEntry: "html"}, []Variant{
		{Entry: htmlEntry, Output: "a.html"},
		{Entry: htmlEntry, Output: "b.html"},
		{Entry: htmlEntry, Output: "a.html"},
	}, Options{})
	requireClean(t, res.Diagnostics)
}

// TestHTMLHasNoAppTier: "app-tier" means valid Blade that real Laravel could
// run instead. Real Laravel cannot run an .html view, so there is no tier to
// fall to — in an .html file the same constructs are errors, and the message
// says what the HTML syntax can do rather than naming a PHP tier.
func TestHTMLHasNoAppTier(t *testing.T) {
	site := map[string]string{"resources/data/site.json": `{"n": 2, "a": true, "list": [1]}`}
	pages := map[string]string{
		`<Foreach site.list="x">{{ loop.parent }}</Foreach>`: "member .parent is not available — ",
		`{{ site.list.first() }}`:                            "method call (.first()) — ",
		`{{ route('home') }}`:                                "function call route()",
		`{{ site.a ? 'x' : 'y' }}`:                           "",
		`{{ site.n + 1 }}`:                                   "",
		`<If condition="count(site)">x</If>`:                 "function call count()",
		`<Vite src="resources/js/app.js" />`:                 `<Vite> js entry "resources/js/app.js" needs a real bundler; skipped`,
		`<Card :title="site.n * 2" />`:                       "",
		`<Foreach site.n="x" limit="1 + 1">x</Foreach>`:      "",
	}
	for page, wantPrefix := range pages {
		files := map[string]string{htmlEntry: page, "resources/views/components/card.html": `<Props title />{{ title }}`}
		for k, v := range site {
			files[k] = v
		}
		_, diags := htmlPage(t, files)
		if len(diags) == 0 {
			t.Errorf("%s: want a diagnostic", page)
		}
		for _, d := range diags {
			if d.Kind != KindError {
				t.Errorf("%s: kind = %s (%s); an .html file has no app tier", page, d.Kind, d.Message)
			}
			if strings.Contains(d.Message, "app tier") || strings.Contains(d.Message, "PHP") {
				t.Errorf("%s: message %q names a tier an .html file does not have", page, d.Message)
			}
			if !strings.HasPrefix(d.Message, wantPrefix) {
				t.Errorf("%s: message = %q; want it to start %q", page, d.Message, wantPrefix)
			}
		}
	}

	// An expression the HTML syntax cannot evaluate says what it can.
	_, diags := htmlPage(t, map[string]string{htmlEntry: `{{ route('home') }}`})
	want := "function call route() — a template can read values and compare them (==, !=, <, >, and, or, not), nothing more"
	if len(diags) != 1 || diags[0].Message != want || diags[0].Construct != "expression" {
		t.Errorf("diags = %+v; want one expression error %q", diags, want)
	}

	// Blade is untouched: the same page in Blade still falls to the app tier.
	res := Compile(vfs.Mem{bladeEntry: `{{ route('home') }}`}, []string{bladeEntry}, Options{})
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Kind != KindAppTier || res.Diagnostics[0].Message != "function call route()" {
		t.Errorf("blade diags = %+v; want one app-tier diagnostic, worded as before", res.Diagnostics)
	}
}

// TestHTMLBarePropIsOptional: <Props badge /> names a prop without making it
// required. A component tag that leaves it out renders it empty — the way
// props work in every component system an HTML author has met — where Blade's
// @props(['badge']) would be an undefined variable.
func TestHTMLBarePropIsOptional(t *testing.T) {
	out, diags := htmlPage(t, map[string]string{
		htmlEntry:                              `<Card title="A" badge="New" />|<Card title="B" />`,
		"resources/views/components/card.html": `<Props title badge /><b>{{ title }}</b><If badge> [{{ badge }}]</If>{{ badge ?? 'none' }}`,
	})
	requireClean(t, diags)
	if want := `<b>A</b> [New]New|<b>B</b>none`; out != want {
		t.Errorf("output = %q; want %q", out, want)
	}
}
