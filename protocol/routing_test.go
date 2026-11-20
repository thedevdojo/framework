package protocol

import (
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

func routingTree() vfs.FS {
	return vfs.Mem{
		"src/pages/index.blade.php":       "home",
		"src/pages/about.blade.php":       "about-flat",
		"src/pages/about/index.blade.php": "about-nested",
		"src/pages/blog/index.blade.php":  "blog",
		"src/pages/404.blade.php":         "not found",
	}
}

// TestResolveURL pins the URL→page routing table promised in spec §2: root maps
// to index, a flat file wins over a same-named directory index, a directory
// index serves its path, and an unmatched URL falls back to the 404 page.
func TestResolveURL(t *testing.T) {
	fs := routingTree()
	cases := []struct {
		url   string
		entry string
	}{
		{"/", "src/pages/index.blade.php"},
		{"", "src/pages/index.blade.php"},
		{"/about", "src/pages/about.blade.php"},     // flat wins over about/index
		{"/about/", "src/pages/about.blade.php"},    // trailing slash ignored
		{"/blog", "src/pages/blog/index.blade.php"}, // directory index
		{"/missing", "src/pages/404.blade.php"},     // 404 fallback
	}
	for _, c := range cases {
		entry, ok := ResolveURL(fs, c.url)
		if !ok || entry != c.entry {
			t.Errorf("ResolveURL(%q) = (%q, %v), want (%q, true)", c.url, entry, ok, c.entry)
		}
	}
}

// TestResolveURLNo404: with no 404 page, an unmatched URL resolves to nothing.
func TestResolveURLNo404(t *testing.T) {
	fs := vfs.Mem{"src/pages/index.blade.php": "home"}
	if entry, ok := ResolveURL(fs, "/missing"); ok {
		t.Errorf("ResolveURL(/missing) = (%q, true), want (\"\", false) without a 404 page", entry)
	}
}

// TestResolveURLTraversalGuarded: a URL that would escape the pages root
// resolves to nothing (no 404 page present), never a file outside pages/.
func TestResolveURLTraversalGuarded(t *testing.T) {
	fs := vfs.Mem{"src/pages/index.blade.php": "home"}
	if entry, ok := ResolveURL(fs, "/../secret"); ok {
		t.Errorf("ResolveURL(/../secret) = (%q, true), want (\"\", false)", entry)
	}
}

// TestResolveURLHTMLPages: a Studio-syntax page routes exactly like a Blade
// one. When a URL has both, the Blade page serves it — the same order the
// renderer's page-collision diagnostic names them in.
func TestResolveURLHTMLPages(t *testing.T) {
	fs := vfs.Mem{
		"src/pages/index.html":           "home",
		"src/pages/about.html":           "about-flat",
		"src/pages/about/index.html":     "about-nested",
		"src/pages/blog/index.html":      "blog",
		"src/pages/pricing.blade.php":    "pricing-blade",
		"src/pages/pricing.html":         "pricing-html",
		"src/pages/docs/index.blade.php": "docs-nested-blade",
		"src/pages/docs.html":            "docs-flat-html",
		"src/pages/404.html":             "not found",
	}
	cases := []struct{ url, entry string }{
		{"/", "src/pages/index.html"},
		{"/about", "src/pages/about.html"},     // flat wins over about/index
		{"/blog", "src/pages/blog/index.html"}, // directory index
		{"/pricing", "src/pages/pricing.blade.php"},
		{"/docs", "src/pages/docs.html"}, // a flat page still wins over a directory index
		{"/missing", "src/pages/404.html"},
	}
	for _, c := range cases {
		entry, ok := ResolveURL(fs, c.url)
		if !ok || entry != c.entry {
			t.Errorf("ResolveURL(%q) = (%q, %v), want (%q, true)", c.url, entry, ok, c.entry)
		}
	}
}

// TestDiscoverPagesBothSyntaxes: a page is a .blade.php or an .html file under
// pages/ — nothing else in that folder is.
func TestDiscoverPagesBothSyntaxes(t *testing.T) {
	fs := vfs.Mem{
		"src/pages/index.html":         "",
		"src/pages/about.blade.php":    "",
		"src/pages/blog/post.html":     "",
		"src/pages/notes.md":           "",
		"src/pages/hero.yml":           "",
		"src/components/button.html":   "",
		"src/components/nav.blade.php": "",
	}
	got := DiscoverPages(fs)
	want := []string{
		"src/pages/about.blade.php",
		"src/pages/blog/post.html",
		"src/pages/index.html",
	}
	if len(got) != len(want) {
		t.Fatalf("DiscoverPages = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DiscoverPages = %v; want %v", got, want)
		}
	}
}
