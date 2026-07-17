package protocol

import (
	"testing"

	"github.com/thedevdojo/pocketknife/vfs"
)

func routingTree() vfs.FS {
	return vfs.Mem{
		"resources/views/pages/index.blade.php":       "home",
		"resources/views/pages/about.blade.php":       "about-flat",
		"resources/views/pages/about/index.blade.php": "about-nested",
		"resources/views/pages/blog/index.blade.php":  "blog",
		"resources/views/pages/404.blade.php":         "not found",
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
		{"/", "resources/views/pages/index.blade.php"},
		{"", "resources/views/pages/index.blade.php"},
		{"/about", "resources/views/pages/about.blade.php"},     // flat wins over about/index
		{"/about/", "resources/views/pages/about.blade.php"},    // trailing slash ignored
		{"/blog", "resources/views/pages/blog/index.blade.php"}, // directory index
		{"/missing", "resources/views/pages/404.blade.php"},     // 404 fallback
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
	fs := vfs.Mem{"resources/views/pages/index.blade.php": "home"}
	if entry, ok := ResolveURL(fs, "/missing"); ok {
		t.Errorf("ResolveURL(/missing) = (%q, true), want (\"\", false) without a 404 page", entry)
	}
}

// TestResolveURLTraversalGuarded: a URL that would escape the pages root
// resolves to nothing (no 404 page present), never a file outside pages/.
func TestResolveURLTraversalGuarded(t *testing.T) {
	fs := vfs.Mem{"resources/views/pages/index.blade.php": "home"}
	if entry, ok := ResolveURL(fs, "/../secret"); ok {
		t.Errorf("ResolveURL(/../secret) = (%q, true), want (\"\", false)", entry)
	}
}
