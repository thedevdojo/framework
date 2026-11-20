package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

// shapeSite is one small site (a page, a component, the site object and a
// collection) written into the folders a shape names.
func shapeSite(root, pages, components string) (vfs.Mem, string) {
	entry := pages + "index.blade.php"
	return vfs.Mem{
		entry:                                "<x-card :title=\"$site->name\"/>\n@foreach ($plans as $plan)\n{{ $plan->name }}\n@endforeach\n",
		components + "card.blade.php":        "@props(['title'])\n<b>{{ $title }}</b>\n",
		root + "data/site.json":              `{"name": "Acme"}`,
		root + "data/collections/plans.json": `[{"name": "Starter"}, {"name": "Pro"}]`,
		root + "data/content/notes/first.md": "---\ntitle: First\n---\nHello\n",
	}, entry
}

func noticeCodes(notices []site.Notice) []string {
	out := []string{}
	for _, n := range notices {
		out = append(out, n.Code)
	}
	return out
}

func TestEveryFolderShapeRendersTheSameBytes(t *testing.T) {
	old, oldEntry := shapeSite("resources/", "resources/views/pages/", "resources/views/components/")
	want := Compile(old, []string{oldEntry}, Options{})
	if len(want.Diagnostics) > 0 {
		t.Fatalf("the old shape should be clean: %+v", want.Diagnostics)
	}
	page := want.Files["index.html"]
	if !strings.Contains(page, "<b>Acme</b>") || !strings.Contains(page, "Pro") {
		t.Fatalf("the old shape did not render its component and data: %q", page)
	}

	for name, shape := range map[string][3]string{
		"src, flat":        {"src/", "src/pages/", "src/components/"},
		"src, under views": {"src/", "src/views/pages/", "src/views/components/"},
		"src, pages moved": {"src/", "src/pages/", "src/views/components/"},
		"resources, flat":  {"resources/", "resources/pages/", "resources/components/"},
	} {
		t.Run(name, func(t *testing.T) {
			fs, entry := shapeSite(shape[0], shape[1], shape[2])
			got := Compile(fs, []string{entry}, Options{})
			if len(got.Diagnostics) > 0 {
				t.Fatalf("diagnostics: %+v", got.Diagnostics)
			}
			if !reflect.DeepEqual(got.Files, want.Files) {
				t.Fatalf("files = %q, want %q", got.Files, want.Files)
			}
		})
	}
}

func TestACompileCarriesTheSitesNotices(t *testing.T) {
	flat, entry := shapeSite("src/", "src/pages/", "src/components/")
	if got := Compile(flat, []string{entry}, Options{}).Notices; len(got) != 0 {
		t.Fatalf("a flat src site has nothing to be told: %+v", got)
	}

	old, entry := shapeSite("resources/", "resources/views/pages/", "resources/views/components/")
	got := noticeCodes(Compile(old, []string{entry}, Options{}).Notices)
	if !reflect.DeepEqual(got, []string{site.CodeResourcesFolder}) {
		t.Fatalf("notices = %v", got)
	}
}

func TestAViteEntryNamedInTheOldFolderIsReadFromSrc(t *testing.T) {
	fs := vfs.Mem{
		"src/pages/index.blade.php": "@vite(['resources/css/site.css'])\n",
		"src/css/site.css":          "body { color: red }",
	}
	res := Compile(fs, []string{"src/pages/index.blade.php"}, Options{})

	if len(res.Diagnostics) > 0 {
		t.Fatalf("diagnostics: %+v", res.Diagnostics)
	}
	if !strings.Contains(res.Files["index.html"], "body { color: red }") {
		t.Fatalf("the stylesheet was not inlined: %q", res.Files["index.html"])
	}
	if got := noticeCodes(res.Notices); !reflect.DeepEqual(got, []string{site.CodeAssetPath}) {
		t.Fatalf("notices = %v", got)
	}
	if n := res.Notices[0]; n.File != "src/pages/index.blade.php" {
		t.Fatalf("the notice should name the file to change: %+v", n)
	}
	if deps := res.Deps["index.html"]; !reflect.DeepEqual(deps, []string{"src/pages/index.blade.php", "src/css/site.css"}) {
		t.Fatalf("deps = %v", deps)
	}
}

func TestAFolderThatIsNotReadContributesNothing(t *testing.T) {
	fs, entry := shapeSite("src/", "src/pages/", "src/components/")
	// Leftovers from before the move. None of it may reach the page.
	fs["resources/data/site.json"] = `{"name": "Stale"}`
	fs["resources/data/collections/plans.json"] = `[{"name": "Gone"}]`
	fs["src/views/components/card.blade.php"] = "@props(['title'])\n<i>stale</i>\n"

	res := Compile(fs, []string{entry}, Options{})
	page := res.Files["index.html"]
	if strings.Contains(page, "Stale") || strings.Contains(page, "Gone") || strings.Contains(page, "stale") {
		t.Fatalf("a folder that is not read leaked into the page: %q", page)
	}
	if got := noticeCodes(res.Notices); !reflect.DeepEqual(got, []string{site.CodeFolderNotRead, site.CodeFolderNotRead}) {
		t.Fatalf("notices = %v", got)
	}
}
