package site

import (
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

// files builds an in-memory site out of paths; the contents do not matter to
// where a site's folders are.
func files(paths ...string) vfs.Mem {
	m := vfs.Mem{}
	for _, p := range paths {
		m[p] = "x"
	}
	return m
}

func codes(l Layout) []string {
	var out []string
	for _, n := range l.Notices {
		out = append(out, n.Code)
	}
	return out
}

func sameCodes(t *testing.T, l Layout, want ...string) {
	t.Helper()
	got := codes(l)
	if len(got) != len(want) {
		t.Fatalf("notices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("notices = %v, want %v", got, want)
		}
	}
}

func TestAFlatSrcSiteIsTheDefaultShape(t *testing.T) {
	l := Detect(files("src/pages/index.html", "src/components/card.html", "src/data/site.json"))

	if l.Root != "src/" || l.Pages != "src/pages/" || l.Components != "src/components/" {
		t.Fatalf("got root %q pages %q components %q", l.Root, l.Pages, l.Components)
	}
	if l.SiteData() != "src/data/site.json" || l.Collections() != "src/data/collections/" || l.Content() != "src/data/content/" {
		t.Fatalf("data paths: %q %q %q", l.SiteData(), l.Collections(), l.Content())
	}
	sameCodes(t, l)
}

func TestAnEmptySiteLooksInSrc(t *testing.T) {
	l := Detect(files())

	if l.Pages != "src/pages/" || l.Components != "src/components/" || l.SiteData() != "src/data/site.json" {
		t.Fatalf("got pages %q components %q site %q", l.Pages, l.Components, l.SiteData())
	}
	sameCodes(t, l)
}

func TestTheOldResourcesShapeStillWorksWithOneNotice(t *testing.T) {
	l := Detect(files(
		"resources/views/pages/index.blade.php",
		"resources/views/components/card.blade.php",
		"resources/data/site.json",
	))

	if l.Root != "resources/" || l.Pages != "resources/views/pages/" || l.Components != "resources/views/components/" {
		t.Fatalf("got root %q pages %q components %q", l.Root, l.Pages, l.Components)
	}
	if l.SiteData() != "resources/data/site.json" {
		t.Fatalf("site data = %q", l.SiteData())
	}
	// One notice for the site: the old root already says the whole shape is
	// old, so views/ is not reported a second time.
	sameCodes(t, l, "resources-folder")
	if l.Notices[0].Kind != "deprecation" || l.Notices[0].Message == "" {
		t.Fatalf("notice = %+v", l.Notices[0])
	}
}

func TestViewsInsideSrcIsABackup(t *testing.T) {
	l := Detect(files("src/views/pages/index.html", "src/views/components/card.html"))

	if l.Pages != "src/views/pages/" || l.Components != "src/views/components/" {
		t.Fatalf("got pages %q components %q", l.Pages, l.Components)
	}
	sameCodes(t, l, "views-folder")
}

func TestPagesAndComponentsAreFoundSeparately(t *testing.T) {
	// Pages were moved, components were not yet.
	l := Detect(files("src/pages/index.html", "src/views/components/card.html"))

	if l.Pages != "src/pages/" || l.Components != "src/views/components/" {
		t.Fatalf("got pages %q components %q", l.Pages, l.Components)
	}
	sameCodes(t, l, "views-folder")
}

func TestSrcWinsOverResourcesAndSaysSo(t *testing.T) {
	l := Detect(files("src/pages/index.html", "resources/views/pages/old.blade.php"))

	if l.Root != "src/" || l.Pages != "src/pages/" {
		t.Fatalf("got root %q pages %q", l.Root, l.Pages)
	}
	sameCodes(t, l, "folder-not-read")
}

func TestTheFlatFolderWinsOverViews(t *testing.T) {
	l := Detect(files("src/pages/index.html", "src/views/pages/old.html"))

	if l.Pages != "src/pages/" {
		t.Fatalf("pages = %q", l.Pages)
	}
	sameCodes(t, l, "folder-not-read")
}

func TestAFlatSiteInsideResourcesIsReadToo(t *testing.T) {
	l := Detect(files("resources/pages/index.html", "resources/components/card.html"))

	if l.Pages != "resources/pages/" || l.Components != "resources/components/" {
		t.Fatalf("got pages %q components %q", l.Pages, l.Components)
	}
	sameCodes(t, l, "resources-folder")
}

func TestOutputNameDropsWhicheverFolderAPageIsIn(t *testing.T) {
	for entry, want := range map[string]string{
		"src/pages/index.html":                         "index.html",
		"src/pages/about.blade.php":                    "about.html",
		"src/views/pages/blog/index.html":              "blog/index.html",
		"resources/views/pages/about.blade.php":        "about.html",
		"resources/pages/about.html":                   "about.html",
		"src/components/sections/hero.html":            "components/sections/hero.html",
		"src/views/components/sections/hero.blade.php": "components/sections/hero.html",
		"resources/views/components/card.blade.php":    "components/card.html",
		"somewhere/else.html":                          "somewhere/else.html",
	} {
		if got := OutputName(entry); got != want {
			t.Errorf("OutputName(%q) = %q, want %q", entry, got, want)
		}
	}
}

func TestAnAssetNamedInTheOtherFolderIsFoundInThisOne(t *testing.T) {
	src := Detect(files("src/pages/index.html", "src/css/site.css"))
	if got, moved := src.Asset("resources/css/site.css"); got != "src/css/site.css" || !moved {
		t.Fatalf("in a src site: %q %v", got, moved)
	}
	if got, moved := src.Asset("src/css/site.css"); got != "src/css/site.css" || moved {
		t.Fatalf("already right: %q %v", got, moved)
	}
	if got, moved := src.Asset("public/css/app.css"); got != "public/css/app.css" || moved {
		t.Fatalf("outside the source folder: %q %v", got, moved)
	}

	old := Detect(files("resources/views/pages/index.blade.php"))
	if got, moved := old.Asset("src/css/site.css"); got != "resources/css/site.css" || !moved {
		t.Fatalf("in a resources site: %q %v", got, moved)
	}
}

func TestAFolderWithNoFilesYetFollowsTheOtherOne(t *testing.T) {
	// A site with pages and no components yet: a component it asks for is
	// looked for beside the pages, not in a shape the site does not use.
	old := Detect(files("resources/views/pages/index.blade.php"))
	if old.Components != "resources/views/components/" {
		t.Fatalf("components = %q", old.Components)
	}
	views := Detect(files("src/views/components/card.html"))
	if views.Pages != "src/views/pages/" {
		t.Fatalf("pages = %q", views.Pages)
	}
	flat := Detect(files("resources/pages/index.html"))
	if flat.Components != "resources/components/" {
		t.Fatalf("components = %q", flat.Components)
	}
}

func TestAResourcesSiteWithNoViewsYetKeepsItsOldShape(t *testing.T) {
	l := Detect(files("resources/data/site.json"))
	if l.Pages != "resources/views/pages/" || l.Components != "resources/views/components/" {
		t.Fatalf("got pages %q components %q", l.Pages, l.Components)
	}
	sameCodes(t, l, "resources-folder")
}
