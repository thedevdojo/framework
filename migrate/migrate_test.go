package migrate

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

// oldSite is a site in the shape every site had before src/: resources/, with
// pages and components under views/.
func oldSite() vfs.Mem {
	return vfs.Mem{
		"resources/views/pages/index.blade.php":             "<x-layouts.main>\n<x-card :title=\"$site->name\"/>\n</x-layouts.main>\n",
		"resources/views/pages/about.html":                  "<Layout src=\"main.html\"><h1>About</h1></Layout>\n",
		"resources/views/components/layouts/main.blade.php": "<html><head>@vite(['resources/css/site.css'])</head><body>{{ $slot }}</body></html>\n",
		"resources/views/components/layouts/main.html":      "<html><head><Vite src=\"resources/css/site.css\" /></head><body><Slot /></body></html>\n",
		"resources/views/components/card.blade.php":         "@props(['title'])\n<b>{{ $title }}</b>\n",
		"resources/views/components/card.yml":               "# fields for resources/views/components/card.blade.php\ntitle: Card\n",
		"resources/css/site.css":                            "body { color: red }",
		"resources/data/site.json":                          `{"name": "Acme"}`,
		"resources/data/content/notes/first.md":             "---\ntitle: First\n---\nSee resources/css/site.css\n",
		"resources/design.md":                               "Pages live in resources/views/pages and data in resources/data.\n",
		"public/images/logo.svg":                            "<svg/>",
	}
}

func compileAll(fs vfs.FS) render.Result {
	var pages []string
	for _, p := range fs.List(site.Detect(fs).Pages) {
		pages = append(pages, p)
	}
	return render.Compile(fs, pages, render.Options{})
}

func TestAnOldSiteMovesToTheFlatSrcShape(t *testing.T) {
	res := Site(oldSite(), render.Options{})
	if len(res.Problems) > 0 {
		t.Fatalf("problems: %+v", res.Problems)
	}

	got := map[string]string{}
	for _, m := range res.Moves {
		got[m.From] = m.To
	}
	want := map[string]string{
		"resources/views/pages/index.blade.php":             "src/pages/index.blade.php",
		"resources/views/pages/about.html":                  "src/pages/about.html",
		"resources/views/components/layouts/main.blade.php": "src/components/layouts/main.blade.php",
		"resources/views/components/layouts/main.html":      "src/components/layouts/main.html",
		"resources/views/components/card.blade.php":         "src/components/card.blade.php",
		"resources/views/components/card.yml":               "src/components/card.yml",
		"resources/css/site.css":                            "src/css/site.css",
		"resources/data/site.json":                          "src/data/site.json",
		"resources/data/content/notes/first.md":             "src/data/content/notes/first.md",
		"resources/design.md":                               "src/design.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("moves = %v\nwant    %v", got, want)
	}
}

func TestAMovedSiteRendersTheSameBytesAndHasNothingLeftToBeTold(t *testing.T) {
	before := oldSite()
	res := Site(before, render.Options{})
	after := res.Apply(before)

	was, is := compileAll(before), compileAll(vfs.Mem(after))
	if len(was.Files) != 2 || !reflect.DeepEqual(was.Files, is.Files) {
		t.Fatalf("before %q\nafter  %q", was.Files, is.Files)
	}
	if len(is.Diagnostics) > 0 || len(is.Notices) > 0 {
		t.Fatalf("after the move: diagnostics %+v notices %+v", is.Diagnostics, is.Notices)
	}
	if after["public/images/logo.svg"] != "<svg/>" {
		t.Fatalf("a file outside the source folder was touched")
	}
	for p := range after {
		if strings.HasPrefix(p, "resources/") {
			t.Fatalf("%s was left behind", p)
		}
	}
}

func TestStylesheetEntriesAreRewrittenInTheSyntaxThatReadsThem(t *testing.T) {
	before := oldSite()
	after := Site(before, render.Options{}).Apply(before)

	if got := after["src/components/layouts/main.blade.php"]; !strings.Contains(got, "@vite(['src/css/site.css'])") {
		t.Errorf("blade layout = %q", got)
	}
	if got := after["src/components/layouts/main.html"]; !strings.Contains(got, `<Vite src="src/css/site.css" />`) {
		t.Errorf("html layout = %q", got)
	}
}

func TestNotesAboutTheSiteAreBroughtUpToDateButItsContentIsNot(t *testing.T) {
	before := oldSite()
	after := Site(before, render.Options{}).Apply(before)

	if got := after["src/design.md"]; got != "Pages live in src/pages and data in src/data.\n" {
		t.Errorf("design.md = %q", got)
	}
	if got := after["src/components/card.yml"]; !strings.Contains(got, "# fields for src/components/card.blade.php") {
		t.Errorf("card.yml = %q", got)
	}
	// Markdown under data/ is the site's content: it is printed on a page, so
	// a path a writer typed there stays as they typed it.
	if got := after["src/data/content/notes/first.md"]; !strings.Contains(got, "See resources/css/site.css") {
		t.Errorf("content was rewritten: %q", got)
	}
}

func TestAFlatSrcSiteHasNothingToMove(t *testing.T) {
	res := Site(vfs.Mem{"src/pages/index.html": "<h1>Hi</h1>\n", "src/components/card.html": "<b>x</b>\n"}, render.Options{})
	if len(res.Moves) != 0 || len(res.Files) != 0 || len(res.Problems) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestOnlyTheViewsFolderMovesInASrcSite(t *testing.T) {
	res := Site(vfs.Mem{
		"src/views/pages/index.html":     "<Card />\n",
		"src/views/components/card.html": "<b>x</b>\n",
		"src/data/site.json":             "{}",
	}, render.Options{})

	got := map[string]string{}
	for _, m := range res.Moves {
		got[m.From] = m.To
	}
	want := map[string]string{
		"src/views/pages/index.html":     "src/pages/index.html",
		"src/views/components/card.html": "src/components/card.html",
	}
	if len(res.Problems) > 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("problems %+v moves %v", res.Problems, got)
	}
}

func TestAMoveThatWouldChangeAPageIsNotMade(t *testing.T) {
	fs := vfs.Mem{
		// @@vite is an escaped directive: Blade prints it as text. Rewriting
		// the path inside it would change what the page says.
		"resources/views/pages/index.blade.php": "<p>Write @@vite(['resources/css/site.css']) in your layout.</p>\n",
	}
	res := Site(fs, render.Options{})

	if len(res.Problems) == 0 {
		t.Fatalf("expected the move to be refused; moves: %+v", res.Moves)
	}
	if len(res.Moves) != 0 || len(res.Files) != 0 {
		t.Fatalf("a refused move must change nothing: %+v", res)
	}
	if !strings.Contains(res.Problems[0].Message, "would render differently") {
		t.Fatalf("problem = %+v", res.Problems[0])
	}
}

func TestAFolderThatIsNotReadIsLeftWhereItIs(t *testing.T) {
	res := Site(vfs.Mem{
		"src/views/pages/index.html":            "<h1>Hi</h1>\n",
		"resources/views/pages/index.blade.php": "<h1>Old</h1>\n",
	}, render.Options{})

	for _, m := range res.Moves {
		if strings.HasPrefix(m.From, "resources/") {
			t.Fatalf("moved a file from a folder the site does not read: %+v", m)
		}
	}
	if len(res.Moves) != 1 || res.Moves[0].To != "src/pages/index.html" {
		t.Fatalf("moves = %+v", res.Moves)
	}
}
