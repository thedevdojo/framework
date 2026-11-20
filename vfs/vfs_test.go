package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemReadAndList(t *testing.T) {
	fs := Mem{
		"src/pages/index.blade.php": "<h1>Hi</h1>",
		"src/pages/about.blade.php": "<h1>About</h1>",
		"src/data/site.json":        `{"name":"X"}`,
	}
	b, ok := fs.Read("src/pages/index.blade.php")
	if !ok || string(b) != "<h1>Hi</h1>" {
		t.Fatalf("Read = %q, %v", b, ok)
	}
	if _, ok := fs.Read("missing"); ok {
		t.Fatal("missing path must return ok=false")
	}
	got := fs.List("src/pages/")
	want := []string{
		"src/pages/about.blade.php",
		"src/pages/index.blade.php",
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List = %v, want %v", got, want)
	}
}

// TestDirReadInRoot: a normal in-root read still works, and paths that escape
// the root (via "../" or an absolute path) return not-found rather than reaching
// a file outside the site tree.
func TestDirReadInRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A secret sitting OUTSIDE the root (a sibling directory entry).
	secret := filepath.Join(filepath.Dir(root), "vfs-secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(secret)

	d := Dir(root)
	if b, ok := d.Read("inside.txt"); !ok || string(b) != "in" {
		t.Fatalf("Read(inside.txt) = %q, %v; want \"in\", true", b, ok)
	}
	for _, p := range []string{
		"../" + filepath.Base(secret),
		"../../etc/hosts",
		"/etc/hosts",
	} {
		if b, ok := d.Read(p); ok {
			t.Errorf("Read(%q) = %q, true; want not-found (must not escape root)", p, b)
		}
	}
}

// TestDirListStaysInRoot: List returns paths relative to the root for an in-root
// prefix, and a traversing prefix lists nothing — never files above the root.
func TestDirListStaysInRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "index.blade.php"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := Dir(root)
	got := d.List("src/pages/")
	if len(got) != 1 || got[0] != "src/pages/index.blade.php" {
		t.Fatalf("List = %v; want [src/pages/index.blade.php]", got)
	}
	for _, prefix := range []string{"../", "../../"} {
		if out := d.List(prefix); len(out) != 0 {
			t.Errorf("List(%q) = %v; want empty (must not escape root)", prefix, out)
		}
	}
}
