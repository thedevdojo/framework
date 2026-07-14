package vfs

import "testing"

func TestMemReadAndList(t *testing.T) {
	fs := Mem{
		"resources/views/pages/index.blade.html": "<h1>Hi</h1>",
		"resources/views/pages/about.blade.html": "<h1>About</h1>",
		"resources/data/site.json":               `{"name":"X"}`,
	}
	b, ok := fs.Read("resources/views/pages/index.blade.html")
	if !ok || string(b) != "<h1>Hi</h1>" {
		t.Fatalf("Read = %q, %v", b, ok)
	}
	if _, ok := fs.Read("missing"); ok {
		t.Fatal("missing path must return ok=false")
	}
	got := fs.List("resources/views/pages/")
	want := []string{
		"resources/views/pages/about.blade.html",
		"resources/views/pages/index.blade.html",
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List = %v, want %v", got, want)
	}
}
