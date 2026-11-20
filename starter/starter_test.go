package starter_test

import (
	"archive/tar"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/protocol"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/starter"
	"github.com/thedevdojo/framework/starter/startertest"
	"github.com/thedevdojo/framework/vfs"
)

// fetcher is a Fetcher that downloads from a test server instead of GitHub.
func fetcher(s *startertest.Server) starter.Fetcher {
	return starter.Fetcher{BaseURL: s.URL}
}

func mustParse(t *testing.T, name string) starter.Template {
	t.Helper()
	tpl, err := starter.Parse(name)
	if err != nil {
		t.Fatalf("Parse(%q): %v", name, err)
	}
	return tpl
}

func TestParseNamesARepositoryOnGitHub(t *testing.T) {
	for in, want := range map[string]string{
		"":                                       "devdojo-templates/starter",
		"starter":                                "devdojo-templates/starter",
		"blog":                                   "devdojo-templates/blog",
		"someone/their-site":                     "someone/their-site",
		"https://github.com/someone/their-site":  "someone/their-site",
		"https://github.com/someone/their-site/": "someone/their-site",
		"https://github.com/someone/their-site.git": "someone/their-site",
		"github.com/someone/their.site":             "someone/their.site",
	} {
		tpl, err := starter.Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got := tpl.Owner + "/" + tpl.Repo; got != want {
			t.Errorf("Parse(%q) = %s, want %s", in, got, want)
		}
		if tpl.String() != "github.com/"+want {
			t.Errorf("Parse(%q).String() = %s", in, tpl.String())
		}
	}
}

func TestParseRefusesWhatIsNotAGitHubRepository(t *testing.T) {
	for _, in := range []string{
		"https://gitlab.com/someone/site",
		"http://github.com/someone/site",
		"https://github.com/someone",
		"https://github.com/someone/site/tree/main",
		"a/b/c",
		"../starter",
		"bad name",
		"someone/..",
		"-flag",
	} {
		if _, err := starter.Parse(in); err == nil {
			t.Errorf("Parse(%q) took it; want an error", in)
		}
	}
}

// The promise `devdojo new` makes: what it writes is a site this version of
// the engine reads with nothing to say, not even a notice.
func TestWriteMakesASiteThatCompilesClean(t *testing.T) {
	server := startertest.Serve(t, map[string][]byte{
		"devdojo-templates/starter": startertest.Archive("starter-HEAD", startertest.Site()),
	})
	dir := filepath.Join(t.TempDir(), "my-site")
	count, err := fetcher(server).Write(mustParse(t, ""), dir)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if count != 6 {
		t.Errorf("Write reported %d files, want 6", count)
	}
	compilesClean(t, dir)
	if got := server.Paths(); len(got) != 1 || got[0] != "/devdojo-templates/starter/tar.gz/HEAD" {
		t.Errorf("asked for %v", got)
	}
}

func compilesClean(t *testing.T, dir string) {
	t.Helper()
	files := vfs.Dir(dir)
	pages := protocol.DiscoverPages(files)
	if len(pages) == 0 {
		t.Fatal("the site has no pages")
	}
	result := render.Compile(files, pages, render.Options{})
	for _, d := range result.Diagnostics {
		t.Errorf("%s:%d:%d [%s] %s", d.File, d.Line, d.Col, d.Kind, d.Message)
	}
	for _, n := range result.Notices {
		t.Errorf("notice: %s", n.Message)
	}
	if _, ok := result.Files["index.html"]; !ok {
		t.Errorf("no home page among the compiled files")
	}
}

// What only the template's listing needs, and what belongs to the
// repository rather than the site, stays behind. .gitignore comes along.
func TestWriteLeavesOutWhatOnlyTheTemplateNeeds(t *testing.T) {
	files := startertest.Site()
	files["public/images/thumbnail.png"] = "a picture the site uses"
	files["public/images/.gitkeep"] = ""
	server := startertest.Serve(t, map[string][]byte{
		"devdojo-templates/starter": startertest.Archive("starter-HEAD", files),
	})
	dir := filepath.Join(t.TempDir(), "my-site")
	if _, err := fetcher(server).Write(mustParse(t, ""), dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for _, gone := range []string{"template.json", "thumbnail.png", "README.md", ".github", ".gitattributes", ".DS_Store", "src/.DS_Store"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s was written; it belongs to the template, not the site", gone)
		}
	}
	for _, kept := range []string{".gitignore", "public/images/thumbnail.png", "public/images/.gitkeep", "src/pages/index.html"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was not written: %v", kept, err)
		}
	}
}

func TestWriteIntoAnEmptyFolderThatExists(t *testing.T) {
	server := startertest.Serve(t, map[string][]byte{
		"devdojo-templates/starter": startertest.Archive("starter-HEAD", startertest.Site()),
	})
	dir := t.TempDir()
	if _, err := fetcher(server).Write(mustParse(t, ""), dir); err != nil {
		t.Fatalf("Write into an empty folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src/pages/index.html")); err != nil {
		t.Errorf("home page not written: %v", err)
	}
	if leftovers := strayTemps(t, filepath.Dir(dir)); len(leftovers) > 0 {
		t.Errorf("left behind: %v", leftovers)
	}
}

// A folder with anything in it is refused before anything is downloaded, and
// left exactly as it was.
func TestWriteRefusesAFolderWithSomethingInItBeforeDownloading(t *testing.T) {
	server := startertest.Serve(t, map[string][]byte{
		"devdojo-templates/starter": startertest.Archive("starter-HEAD", startertest.Site()),
	})
	dir := t.TempDir()
	mine := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := fetcher(server).Write(mustParse(t, ""), dir)
	if !errors.Is(err, starter.ErrNotEmpty) {
		t.Fatalf("err = %v, want ErrNotEmpty", err)
	}
	if n := server.Requests.Load(); n != 0 {
		t.Errorf("%d requests were made for a folder that was refused", n)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the folder was written to: %d entries", len(entries))
	}
}

func TestWriteRefusesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taken")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (starter.Fetcher{BaseURL: "http://127.0.0.1:1"}).Write(mustParse(t, ""), path); err == nil {
		t.Fatal("Write over a file succeeded")
	}
}

// Every way a download can go wrong ends with a sentence a person can act
// on, and nothing left on disk: not the folder, not a half-written site.
func TestWriteFailuresSayWhatHappenedAndLeaveNothing(t *testing.T) {
	notASite := map[string]string{"index.html": "<h1>hi</h1>", "README.md": "# hi"}
	server := startertest.Serve(t, map[string][]byte{
		"devdojo-templates/notasite": startertest.Archive("notasite-HEAD", notASite),
		"devdojo-templates/link": startertest.Archive("link-HEAD", startertest.Site(),
			startertest.Entry{Name: "link-HEAD/src/pages/secret.html", Type: tar.TypeSymlink, Linkname: "/etc/passwd"}),
		"devdojo-templates/hardlink": startertest.Archive("hardlink-HEAD", startertest.Site(),
			startertest.Entry{Name: "hardlink-HEAD/src/x.html", Type: tar.TypeLink, Linkname: "hardlink-HEAD/src/pages/index.html"}),
		"devdojo-templates/device": startertest.Archive("device-HEAD", startertest.Site(),
			startertest.Entry{Name: "device-HEAD/public/dev", Type: tar.TypeChar}),
		"devdojo-templates/escape": startertest.Archive("escape-HEAD", startertest.Site(),
			startertest.Entry{Name: "escape-HEAD/../../escaped.txt", Body: "out"}),
		"devdojo-templates/absolute": startertest.Pack(
			startertest.Entry{Name: "/tmp/absolute.txt", Body: "out"}),
		"devdojo-templates/twotops": startertest.Pack(
			startertest.Entry{Name: "a/src/pages/index.html", Body: "<h1>a</h1>"},
			startertest.Entry{Name: "b/src/pages/index.html", Body: "<h1>b</h1>"}),
		"devdojo-templates/notgzip": []byte("<html>this is a web page, not a download</html>"),
	})

	for name, want := range map[string]string{
		"nope":     `there is no template called "nope" at github.com/devdojo-templates/nope`,
		"notasite": "is not a site: it has no src/ or resources/ folder",
		"link":     "src/pages/secret.html is a link",
		"hardlink": "src/x.html is a link",
		"device":   "public/dev is not a file or a folder",
		"escape":   "is outside the template's folder",
		"absolute": "is outside the template's folder",
		"twotops":  "is not a template download",
		"notgzip":  "is not a template download",
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "deep", "my-site")
			_, err := fetcher(server).Write(mustParse(t, name), dir)
			if err == nil {
				t.Fatal("Write succeeded")
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to say %q", err, want)
			}
			if entries, _ := os.ReadDir(parent); len(entries) != 0 {
				t.Errorf("left behind in the parent folder: %v", names(entries))
			}
		})
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), "absolute.txt")); err == nil {
		t.Error("an absolute path in the archive was written")
	}
}

func TestWriteSaysWhenGitHubCannotBeReached(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-site")
	f := starter.Fetcher{BaseURL: "http://127.0.0.1:1", Client: &http.Client{Timeout: 2 * time.Second}}
	_, err := f.Write(mustParse(t, ""), dir)
	if err == nil || !strings.Contains(err.Error(), "couldn't reach GitHub") {
		t.Fatalf("err = %v, want it to say it couldn't reach GitHub", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the folder was made for a download that never came")
	}
}

func TestWriteRefusesATemplateThatIsTooBig(t *testing.T) {
	files := startertest.Site()
	files["public/big.bin"] = strings.Repeat("x", 4096)
	server := startertest.Serve(t, map[string][]byte{
		"devdojo-templates/starter": startertest.Archive("starter-HEAD", files),
	})
	parent := t.TempDir()
	f := starter.Fetcher{BaseURL: server.URL, MaxBytes: 2048}
	_, err := f.Write(mustParse(t, ""), filepath.Join(parent, "my-site"))
	if err == nil || !strings.Contains(err.Error(), "bigger than") {
		t.Fatalf("err = %v, want it to say the template is too big", err)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("left behind: %v", names(entries))
	}
}

// The real starter, downloaded from GitHub, compiles clean and is laid out
// the way sites are now. Off unless DEVDOJO_NETWORK_TESTS=1, so the suite
// never needs the network.
func TestTheRealStarterFromGitHub(t *testing.T) {
	if os.Getenv("DEVDOJO_NETWORK_TESTS") != "1" {
		t.Skip("set DEVDOJO_NETWORK_TESTS=1 to download the real starter from GitHub")
	}
	dir := filepath.Join(t.TempDir(), "my-site")
	if _, err := (starter.Fetcher{BaseURL: "https://codeload.github.com"}).Write(mustParse(t, ""), dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	compilesClean(t, dir)
	if layout := site.Detect(vfs.Dir(dir)); layout.Pages != "src/pages/" {
		t.Errorf("pages folder = %q, want src/pages/", layout.Pages)
	}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".blade.php") {
			t.Errorf("%s is a Blade view; the starter is written in the HTML syntax", p)
		}
		return nil
	})
}

func strayTemps(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".devdojo-new-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}
