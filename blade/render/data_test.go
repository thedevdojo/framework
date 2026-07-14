package render

import (
	"testing"

	"github.com/thedevdojo/pocketknife/vfs"
)

const dataEntry = "resources/views/pages/index.blade.html"

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
// components), even though they do not see collections/content.
func TestSiteVisibleInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-brand/>",
		"resources/views/components/brand.blade.html": "{{ $site->name ?? 'unset' }}",
		siteDataPath: `{"name":"Acme"}`,
	}, nil)
	if got := res.Files["index.html"]; got != "Acme" {
		t.Errorf("component did not see $site: output = %q; want %q", got, "Acme")
	}
}

// TestCollectionsNotVisibleInComponent: a collection binding is page-only; a
// component referencing it sees an undefined variable (guarded here with ??).
func TestCollectionsNotVisibleInComponent(t *testing.T) {
	res := compileOne(t, map[string]string{
		dataEntry: "<x-list/>",
		"resources/views/components/list.blade.html": "{{ $plans ?? 'no-plans' }}",
		collectionsDir + "plans.json":                `[{"name":"Free"}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "no-plans" {
		t.Errorf("collections leaked into component: output = %q", got)
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
		"resources/views/components/brand.blade.html": "comp:{{ $site->name }}",
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
