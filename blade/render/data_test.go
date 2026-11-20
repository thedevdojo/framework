package render

import (
	"strings"
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

const dataEntry = "resources/views/pages/index.blade.php"

// compileOne renders a single page over an in-memory filesystem.
func compileOne(t *testing.T, files map[string]string, data map[string]any) Result {
	t.Helper()
	fs := vfs.Mem(files)
	return Compile(fs, []string{dataEntry}, Options{Data: data})
}

// hasDiagKC reports whether a diagnostic of the given kind+construct exists.
func hasDiagKC(res Result, kind DiagKind, construct string) bool {
	for _, d := range res.Diagnostics {
		if d.Kind == kind && d.Construct == construct {
			return true
		}
	}
	return false
}

// TestSiteMissing: no site.json → $site is an empty object. A guarded access
// falls back and there is no diagnostic.
func TestSiteMissing(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "[{{ $site->name ?? 'unset' }}]",
	}, nil)
	if got := res.Files["index.html"]; got != "[unset]" {
		t.Errorf("output = %q; want %q", got, "[unset]")
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestEmptySiteIsTruthy: an empty site.json ({}) is a stdClass, which is TRUTHY
// (unlike an empty array) — the headline value-model flip. `[]` on it, or a
// missing member, is an error the render tier reports.
func TestEmptySiteIsTruthy(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:    "@if ($site)[Y]@else[N]@endif",
		siteDataPath: `{}`,
	}, nil)
	if got := res.Files["index.html"]; got != "[Y]" {
		t.Errorf("empty $site truthiness: output = %q; want [Y]", got)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}

	// [] on the object is an error/object-as-array, rendering empty.
	res = compileOne(t, map[string]string{
		dataEntry:    "[{{ $site['x'] }}]",
		siteDataPath: `{}`,
	}, nil)
	if got := res.Files["index.html"]; got != "[]" {
		t.Errorf("$site['x'] output = %q; want []", got)
	}
	if !hasDiagKC(res, KindError, "object-as-array") {
		t.Errorf("want object-as-array diagnostic, got %v", res.Diagnostics)
	}
}

// TestSiteMalformed: invalid site.json → site-data error and an empty $site.
func TestSiteMalformed(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:    "[{{ $site->name ?? 'unset' }}]",
		siteDataPath: "{ not json",
	}, nil)
	if got := res.Files["index.html"]; got != "[unset]" {
		t.Errorf("output = %q; want %q", got, "[unset]")
	}
	if !hasDiagKC(res, KindError, "site-data") {
		t.Errorf("want site-data diagnostic, got %v", res.Diagnostics)
	}
}

// TestSiteArrayIsMalformed: site.json must be an object; a top-level array is a
// site-data error with an empty $site.
func TestSiteArrayIsMalformed(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:    "[{{ $site->name ?? 'unset' }}]",
		siteDataPath: `["a","b"]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[unset]" {
		t.Errorf("output = %q", got)
	}
	if !hasDiagKC(res, KindError, "site-data") {
		t.Errorf("want site-data diagnostic, got %v", res.Diagnostics)
	}
}

// TestSitePresent: a well-formed site.json binds $site with property access.
func TestSitePresent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:    "{{ $site->name }}/{{ $site->tagline }}",
		siteDataPath: `{"name":"Acme","tagline":"We build"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "Acme/We build" {
		t.Errorf("output = %q", got)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestSiteVisibleInComponent probes the visibility rule the oracle pins:
// components DO see $site (real Blade's View::share leaks into anonymous
// components).
func TestSiteVisibleInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-brand/>",
		"resources/views/components/brand.blade.php": "{{ $site->name ?? 'unset' }}",
		siteDataPath: `{"name":"Acme"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "Acme" {
		t.Errorf("component did not see $site: output = %q; want %q", got, "Acme")
	}
}

// TestCollectionsVisibleInComponent pins the 2026-07-28 contract change
// (docs/specs/2026-07-28-collections-global-scope-design.md): collections bind
// GLOBALLY, like $site, so a component reads one WITHOUT the page threading it
// through. This test previously asserted the opposite (collections were
// page-only), which is exactly the bug the change fixes — a layout-instantiated
// nav backed by collections/links.json rendered empty.
func TestCollectionsVisibleInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-list/>",
		"resources/views/components/list.blade.php": "@foreach ($plans as $p){{ $p->name }};@endforeach",
		collectionsDir + "plans.json":               `[{"name":"Free"},{"name":"Pro"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "Free;Pro;" {
		t.Errorf("component did not see the collection: output = %q; want %q", got, "Free;Pro;")
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestContentVisibleInComponent: content collections are global on the same
// terms as collections/*.json.
func TestContentVisibleInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-list/>",
		"resources/views/components/list.blade.php": "@foreach ($post as $p){{ $p->title }};@endforeach",
		contentDir + "post/a.md":                    "---\ntitle: A\n---\nbody\n",
		contentDir + "post/b.md":                    "---\ntitle: B\n---\nbody\n",
	}, nil)
	if got := res.Files["index.html"]; got != "A;B;" {
		t.Errorf("component did not see the content collection: output = %q; want %q", got, "A;B;")
	}
}

// TestLayoutSeesCollection: the origin bug. A nav component instantiated from a
// LAYOUT (itself a component) can bind a collection into a child with
// :links="$links" — no page involvement.
func TestLayoutSeesCollection(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-layouts.main>body</x-layouts.main>",
		"resources/views/components/layouts/main.blade.php": "<x-nav :links=\"$links\"/>[{{ $slot }}]",
		"resources/views/components/nav.blade.php":          "@props(['links' => []])@foreach ($links as $l){{ $l->label }};@endforeach",
		collectionsDir + "links.json":                       `[{"label":"Home"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "Home;[body]" {
		t.Errorf("layout could not bind the collection: output = %q; want %q", got, "Home;[body]")
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestPropsDefaultDoesNotShadowCollection: real Blade's compileProps emits
// `$x = $x ?? $default`, so a declared default fills in only where nothing is
// already bound — it can NOT displace a shared collection. Verified against the
// oracle on 2026-07-28; the approved spec had guessed the opposite, and the
// engine follows real Laravel.
func TestPropsDefaultDoesNotShadowCollection(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-nav/>",
		// A shadowing default (null) would make this @foreach iterate nothing and
		// raise a foreach diagnostic; seeing the collection is the contract.
		"resources/views/components/nav.blade.php": "@props(['links' => null])@foreach ($links as $l){{ $l->label }};@endforeach",
		collectionsDir + "links.json":              `[{"label":"Home"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "Home;" {
		t.Errorf("a @props default wrongly shadowed the collection: output = %q; want %q", got, "Home;")
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestPassedAttrBeatsCollection: a PASSED attribute does override a same-named
// shared collection — it arrives as the component view's per-view data, which
// Factory::gatherData merges over shared data.
func TestPassedAttrBeatsCollection(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: `<x-nav :links="$other"/>`,
		"resources/views/components/nav.blade.php": "@props(['links' => null])@foreach ($links as $l){{ $l->label }};@endforeach",
		collectionsDir + "links.json":              `[{"label":"Global"}]`,
		collectionsDir + "other.json":              `[{"label":"Passed"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "Passed;" {
		t.Errorf("passed attr did not beat the collection: output = %q; want %q", got, "Passed;")
	}
}

// TestPassedNullFallsBackToDefault: a passed attribute is per-view data, so it
// overwrites shared data even when its value is null — and the `??` defaults
// pass then fills the hole. Passing null is therefore the one way a caller can
// reach a default that a same-named collection would otherwise occupy. Both
// halves oracle-verified 2026-07-28.
func TestPassedNullFallsBackToDefault(t *testing.T) {
	// No colliding collection: passed null still yields the default.
	res := compileOne(t, map[string]string{
		dataEntry:                                `<x-c :label="null"/>`,
		"resources/views/components/c.blade.php": `@props(['label' => 'Fallback'])[{{ $label }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[Fallback]" {
		t.Errorf("passed null did not fall back to the default: output = %q; want %q", got, "[Fallback]")
	}

	// Colliding collection: the passed null overwrites it, then the default
	// fills in — the collection does NOT come back.
	res = compileOne(t, map[string]string{
		dataEntry:                                `<x-c :links="null"/>`,
		"resources/views/components/c.blade.php": `@props(['links' => 'DefaultString'])[{{ $links }}]`,
		collectionsDir + "links.json":            `[{"label":"Global"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[DefaultString]" {
		t.Errorf("passed null over a collection: output = %q; want %q", got, "[DefaultString]")
	}
}

// TestPassedFalsyKeepsPassedValue: only NULL triggers the defaults pass. An
// empty string or false is a real value and survives (both echo as "").
func TestPassedFalsyKeepsPassedValue(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                                `<x-c :label="''"/>|<x-c :label="false"/>`,
		"resources/views/components/c.blade.php": `@props(['label' => 'Fallback'])[{{ $label }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[]|[]" {
		t.Errorf("a falsy passed value wrongly took the default: output = %q; want %q", got, "[]|[]")
	}
}

// TestSiteBeatsCollectionNamedSite: `site` is shared LAST on both tiers, so a
// collections/site.json can never displace the site object — in a component OR
// on a page.
func TestSiteBeatsCollectionNamedSite(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "page:{{ $site->name }} <x-brand/>",
		"resources/views/components/brand.blade.php": "comp:{{ $site->name }}",
		siteDataPath:                 `{"name":"Acme"}`,
		collectionsDir + "site.json": `[{"name":"Collection"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "page:Acme comp:Acme" {
		t.Errorf("a collection named site displaced $site: output = %q; want %q", got, "page:Acme comp:Acme")
	}
}

// TestOptionsDataStaysPageLocal: the protocol's data map is per-page view data.
// It still wins in the PAGE (a dynamic page's matched entry shadows its
// collection there) and still never reaches a component, which reads the full
// shared collection.
func TestOptionsDataStaysPageLocal(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "page:{{ $plans }}|<x-list/>",
		"resources/views/components/list.blade.php": "comp:@foreach ($plans as $p){{ $p->name }};@endforeach",
		collectionsDir + "plans.json":               `[{"name":"Free"}]`,
	}, map[string]any{"plans": "entry"})
	if got := res.Files["index.html"]; got != "page:entry|comp:Free;" {
		t.Errorf("output = %q; want %q", got, "page:entry|comp:Free;")
	}
}

// TestUnknownVarStillUndefinedInComponent: making collections global must not
// blunt the undefined-variable diagnostic. A name backed by no collection, no
// content directory, and no prop is still an error inside a component.
func TestUnknownVarStillUndefinedInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-list/>",
		"resources/views/components/list.blade.php": "[{{ $nope }}]",
		collectionsDir + "plans.json":               `[{"name":"Free"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[]" {
		t.Errorf("output = %q; want %q", got, "[]")
	}
	if !hasDiagKC(res, KindError, "undefined-variable") {
		t.Errorf("want undefined-variable diagnostic, got %v", res.Diagnostics)
	}
}

// TestCollectionArray: a JSON-array collection binds $<name> and integer prices
// round-trip identically to PHP's json_decode (0, 9, 42 all print bare).
func TestCollectionArray(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                     "@foreach ($plans as $p){{ $p->name }}={{ $p->price }};@endforeach",
		collectionsDir + "plans.json": `[{"name":"Free","price":0},{"name":"Pro","price":9},{"name":"Max","price":42}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "Free=0;Pro=9;Max=42;" {
		t.Errorf("output = %q", got)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestCollectionNonArray: a collection whose JSON is not an array is a
// collection error and the binding is absent.
func TestCollectionNonArray(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                   "{{ $cfg ?? 'absent' }}",
		collectionsDir + "cfg.json": `{"key":"value"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "absent" {
		t.Errorf("output = %q; want absent", got)
	}
	if !hasDiagKC(res, KindError, "collection") {
		t.Errorf("want collection diagnostic, got %v", res.Diagnostics)
	}
}

// TestCollectionInvalidName: a basename that is not a valid variable name is a
// collection error and is skipped.
func TestCollectionInvalidName(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                         "ok",
		collectionsDir + "2bad-name.json": `[1,2,3]`,
	}, nil)
	if !hasDiagKC(res, KindError, "collection") {
		t.Errorf("want collection diagnostic for invalid name, got %v", res.Diagnostics)
	}
}

// TestContentBinding: content markdown binds $<dir> items ordered by filename,
// each with rendered `content` and a derived `link`. Raw echo emits HTML,
// escaped echo escapes it.
func TestContentBinding(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                "@foreach ($post as $p){{ $p->link }}|{!! $p->content !!}|{{ $p->content }};@endforeach",
		contentDir + "post/b.md": "---\ntitle: B\n---\n**two**\n",
		contentDir + "post/a.md": "---\ntitle: A\n---\n**one**\n",
	}, nil)
	want := "/post/a|<p><strong>one</strong></p>\n|&lt;p&gt;&lt;strong&gt;one&lt;/strong&gt;&lt;/p&gt;\n;" +
		"/post/b|<p><strong>two</strong></p>\n|&lt;p&gt;&lt;strong&gt;two&lt;/strong&gt;&lt;/p&gt;\n;"
	if got := res.Files["index.html"]; got != want {
		t.Errorf("output =\n%q\nwant\n%q", got, want)
	}
}

// TestContentNested: a content item with non-scalar frontmatter is skipped with
// a content error; the remaining item still binds.
func TestContentNested(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                   "@foreach ($post as $p){{ $p->link }};@endforeach",
		contentDir + "post/good.md": "---\ntitle: Good\n---\nbody\n",
		contentDir + "post/bad.md":  "---\nauthor:\n  name: Bob\n---\nbody\n",
	}, nil)
	if got := res.Files["index.html"]; got != "/post/good;" {
		t.Errorf("output = %q; want %q", got, "/post/good;")
	}
	if !hasDiagKC(res, KindError, "content") {
		t.Errorf("want content diagnostic, got %v", res.Diagnostics)
	}
}

// TestDataOverridesSite: an Options.Data key `site` replaces $site for the page,
// but the component still sees the ORIGINAL site.json (shared, not per-page).
func TestDataOverridesSite(t *testing.T) {
	files := map[string]string{
		dataEntry: "page:{{ $site->name }} <x-brand/>",
		"resources/views/components/brand.blade.php": "comp:{{ $site->name }}",
		siteDataPath: `{"name":"FromJson"}`,
	}
	res := compileOne(t, files, map[string]any{
		"site": map[string]any{"name": "FromData"},
	})
	if got := res.Files["index.html"]; got != "page:FromData comp:FromJson" {
		t.Errorf("output = %q; want %q", got, "page:FromData comp:FromJson")
	}
}

// TestDataOverridesCollection: an Options.Data key wins over a collection of the
// same name (top-level data overrides collisions).
func TestDataOverridesCollection(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                     "{{ $plans }}",
		collectionsDir + "plans.json": `[{"name":"Free"}]`,
	}, map[string]any{"plans": "overridden"})
	if got := res.Files["index.html"]; got != "overridden" {
		t.Errorf("output = %q; want overridden", got)
	}
}

// TestCollectionFolder: a folder of one JSON object per entry binds $<name>
// exactly like the single-file form, ordered by `date` descending and then by
// file name ascending; an entry without a date sorts last.
func TestCollectionFolder(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                             "@foreach ($posts as $p){{ $p->slug }}={{ $p->title }};@endforeach",
		collectionsDir + "posts/zeta.json":    `{"slug":"zeta","title":"Zeta","date":"2026-01-05"}`,
		collectionsDir + "posts/alpha.json":   `{"slug":"alpha","title":"Alpha","date":"2026-01-05"}`,
		collectionsDir + "posts/newest.json":  `{"slug":"newest","title":"Newest","date":"2026-03-01"}`,
		collectionsDir + "posts/undated.json": `{"slug":"undated","title":"Undated"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "newest=Newest;alpha=Alpha;zeta=Zeta;undated=Undated;" {
		t.Errorf("output = %q", got)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestCollectionFolderMatchesFile: the same entries in either storage form
// render byte-identically once the file form is in the folder's order.
func TestCollectionFolderMatchesFile(t *testing.T) {
	page := "@foreach ($posts as $p){{ $p->title }}:{!! $p->content !!}|@endforeach"
	folder := compileOne(t, map[string]string{
		dataEntry:                                page,
		collectionsDir + "posts/a.json":          `{"slug":"a","title":"A","date":"2026-02-01","content":"<p>a</p>"}`,
		collectionsDir + "posts/b.json":          `{"slug":"b","title":"B","date":"2026-01-01","content":"<p>b</p>"}`,
		"resources/views/components/x.blade.php": "",
	}, nil)
	file := compileOne(t, map[string]string{
		dataEntry:                     page,
		collectionsDir + "posts.json": `[{"slug":"a","title":"A","date":"2026-02-01","content":"<p>a</p>"},{"slug":"b","title":"B","date":"2026-01-01","content":"<p>b</p>"}]`,
	}, nil)
	if folder.Files["index.html"] != file.Files["index.html"] {
		t.Errorf("folder = %q, file = %q", folder.Files["index.html"], file.Files["index.html"])
	}
	if folder.Files["index.html"] != "A:<p>a</p>|B:<p>b</p>|" {
		t.Errorf("output = %q", folder.Files["index.html"])
	}
}

// TestCollectionFolderVisibleInComponent: the folder form is shared globally
// like every collection, so a component sees it without a passed attribute.
func TestCollectionFolderVisibleInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-list/>",
		"resources/views/components/list.blade.php": "@foreach ($posts as $p){{ $p->slug }};@endforeach",
		collectionsDir + "posts/one.json":           `{"slug":"one","date":"2026-01-01"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "one;" {
		t.Errorf("output = %q", got)
	}
}

// TestCollectionBothForms: a name stored both as a file and as a folder binds
// the FOLDER only — never a merge — and reports one collection error naming
// both paths so the author removes one.
func TestCollectionBothForms(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                     "@foreach ($posts as $p){{ $p->slug }};@endforeach",
		collectionsDir + "posts.json": `[{"slug":"from-file"}]`,
		collectionsDir + "posts/from-folder.json": `{"slug":"from-folder"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "from-folder;" {
		t.Errorf("output = %q; want the folder's entries only", got)
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "collection" || d.File != collectionsDir+"posts.json" {
		t.Errorf("diagnostic = %#v", d)
	}
	for _, want := range []string{collectionsDir + "posts.json", collectionsDir + "posts/", "remove one"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("message %q does not mention %q", d.Message, want)
		}
	}
}

// TestCollectionFolderNonObject: a folder file that is not a JSON object is a
// collection error and only that entry is skipped.
func TestCollectionFolderNonObject(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                          "@foreach ($posts as $p){{ $p->slug }};@endforeach",
		collectionsDir + "posts/good.json": `{"slug":"good"}`,
		collectionsDir + "posts/list.json": `[{"slug":"list"}]`,
		collectionsDir + "posts/bad.json":  `not json`,
	}, nil)
	if got := res.Files["index.html"]; got != "good;" {
		t.Errorf("output = %q", got)
	}
	if n := len(res.Diagnostics); n != 2 {
		t.Fatalf("diagnostics = %v; want two", res.Diagnostics)
	}
	for _, d := range res.Diagnostics {
		if d.Kind != KindError || d.Construct != "collection" || !strings.Contains(d.Message, "must be a JSON object") {
			t.Errorf("diagnostic = %#v", d)
		}
	}
}

// TestCollectionFolderSlugMismatch: a file whose entry carries a different
// slug still binds (the data is the data) but is reported, because the file
// name is the entry's address for every host tool.
func TestCollectionFolderSlugMismatch(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                             "@foreach ($posts as $p){{ $p->slug ?? 'none' }};@endforeach",
		collectionsDir + "posts/renamed.json": `{"slug":"original"}`,
		collectionsDir + "posts/noslug.json":  `{"title":"No slug at all"}`,
	}, nil)
	if got := res.Files["index.html"]; !strings.Contains(got, "original;") {
		t.Errorf("output = %q; the mismatched entry should still bind", got)
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].File != collectionsDir+"posts/renamed.json" {
		t.Fatalf("diagnostics = %v; want one for renamed.json only", res.Diagnostics)
	}
	if !strings.Contains(res.Diagnostics[0].Message, `slug "original"`) {
		t.Errorf("message = %q", res.Diagnostics[0].Message)
	}
}

// TestCollectionFolderIgnoresDeeperFiles: only direct children of the folder
// are entries; a nested directory is invisible, as it always was.
func TestCollectionFolderIgnoresDeeperFiles(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                                "@foreach ($posts as $p){{ $p->slug }};@endforeach",
		collectionsDir + "posts/one.json":        `{"slug":"one"}`,
		collectionsDir + "posts/nested/two.json": `{"slug":"two"}`,
		collectionsDir + "posts/readme.txt":      `not an entry`,
	}, nil)
	if got := res.Files["index.html"]; got != "one;" {
		t.Errorf("output = %q", got)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestCollectionFolderInvalidName: a folder whose name is not a variable name
// is reported on the folder path and skipped.
func TestCollectionFolderInvalidName(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry:                             "ok",
		collectionsDir + "2bad-name/one.json": `{"slug":"one"}`,
	}, nil)
	if !hasDiagKC(res, KindError, "collection") || res.Diagnostics[0].File != collectionsDir+"2bad-name" {
		t.Errorf("want collection diagnostic on the folder, got %v", res.Diagnostics)
	}
}

// TestCompileVariants: one entry rendered many times, each variant's data
// bound over the shared bindings and written to its own output — a dynamic
// page's fan-out — with the collection loaded once and left untouched.
func TestCompileVariants(t *testing.T) {
	fs := vfs.Mem(map[string]string{
		"resources/views/pages/post/[post.slug].blade.php": "{{ $post->title }} of @foreach ($entries as $e){{ $e->slug }}@endforeach",
		collectionsDir + "post/a.json":                     `{"slug":"a","title":"A","date":"2026-01-02"}`,
		collectionsDir + "post/b.json":                     `{"slug":"b","title":"B","date":"2026-01-01"}`,
	})
	entries := []any{
		map[string]any{"slug": "a", "title": "A"},
		map[string]any{"slug": "b", "title": "B"},
	}
	res := CompileVariants(fs, []Variant{
		{Entry: "resources/views/pages/post/[post.slug].blade.php", Data: map[string]any{"post": entries[0]}, Output: "post/a/index.html"},
		{Entry: "resources/views/pages/post/[post.slug].blade.php", Data: map[string]any{"post": entries[1]}, Output: "post/b/index.html"},
	}, Options{Data: map[string]any{"entries": entries}})
	if got := res.Files["post/a/index.html"]; got != "A of ab" {
		t.Errorf("a = %q", got)
	}
	if got := res.Files["post/b/index.html"]; got != "B of ab" {
		t.Errorf("b = %q", got)
	}
	if len(res.Files) != 2 {
		t.Errorf("files = %v; want the two variant outputs only", res.Files)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if deps := res.Deps["post/a/index.html"]; len(deps) == 0 || deps[0] != "resources/views/pages/post/[post.slug].blade.php" {
		t.Errorf("deps = %v", deps)
	}
}

// TestCompileVariantDefaultOutput: a variant without an output name lands at
// the entry's usual output, and one without data is the plain render.
func TestCompileVariantDefaultOutput(t *testing.T) {
	fs := vfs.Mem(map[string]string{dataEntry: "{{ $greeting ?? 'hi' }}"})
	res := CompileVariants(fs, []Variant{{Entry: dataEntry}}, Options{})
	if got := res.Files["index.html"]; got != "hi" {
		t.Errorf("output = %q", got)
	}
}

// TestCompileVariantEscapingOutput: a variant whose given output walks above
// the root is refused like an escaping entry.
func TestCompileVariantEscapingOutput(t *testing.T) {
	fs := vfs.Mem(map[string]string{dataEntry: "x"})
	res := CompileVariants(fs, []Variant{{Entry: dataEntry, Output: "../escape.html"}}, Options{})
	if len(res.Files) != 0 || !hasDiagKC(res, KindError, "entry-invalid") {
		t.Errorf("files = %v, diagnostics = %v", res.Files, res.Diagnostics)
	}
}
