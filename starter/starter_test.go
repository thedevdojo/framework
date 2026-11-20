package starter_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/protocol"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/starter"
	"github.com/thedevdojo/framework/vfs"
)

// The reason the starter lives inside the binary: a version of the tool
// always writes a site that same version reads. This is the test that keeps
// the promise. Every page of a freshly written starter compiles with nothing
// to say, not even a notice about an older folder name.
func TestWriteMakesASiteThatCompilesClean(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-site")
	count, err := starter.Write(dir)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if count == 0 {
		t.Fatal("Write reported no files")
	}

	files := vfs.Dir(dir)
	pages := protocol.DiscoverPages(files)
	if len(pages) == 0 {
		t.Fatal("the starter has no pages")
	}
	result := render.Compile(files, pages, render.Options{})
	for _, d := range result.Diagnostics {
		t.Errorf("%s:%d:%d [%s] %s", d.File, d.Line, d.Col, d.Kind, d.Message)
	}
	for _, n := range result.Notices {
		t.Errorf("notice: %s", n.Message)
	}
	if _, ok := result.Files["index.html"]; !ok {
		t.Errorf("no home page among the compiled files: %v", keys(result.Files))
	}
}

// A new site is written the way sites are written now: source in src/, views
// in the HTML syntax, and public/ beside src/ for files served as they are.
func TestStarterIsInSrcAndTheHTMLSyntax(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-site")
	if _, err := starter.Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if layout := site.Detect(vfs.Dir(dir)); layout.Pages != "src/pages/" {
		t.Errorf("pages folder = %q, want src/pages/", layout.Pages)
	}
	for _, want := range []string{"src/pages/index.html", "src/components/layouts/main.html", "src/data/site.json", "src/css/site.css", "public/favicon.svg"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	err := fs.WalkDir(starter.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".blade.php") {
			t.Errorf("%s is a Blade view; the starter is written in the HTML syntax", p)
		}
		if strings.HasPrefix(p, "resources/") {
			t.Errorf("%s is under resources/; the starter keeps its source in src/", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The starter is a copy of the template DevDojo lists. What only the listing
// needs (its manifest, its thumbnail, the template's own README) and what
// belongs to a checkout stays behind.
func TestStarterLeavesOutWhatOnlyTheTemplateNeeds(t *testing.T) {
	for _, name := range []string{"template.json", "thumbnail.png", "README.md", ".git", ".DS_Store"} {
		if _, err := fs.Stat(starter.FS(), name); err == nil {
			t.Errorf("%s is in the starter; it should not be written into a new site", name)
		}
	}
}

func TestWriteIntoAnEmptyFolderThatExists(t *testing.T) {
	dir := t.TempDir()
	if _, err := starter.Write(dir); err != nil {
		t.Fatalf("Write into an empty folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src/pages/index.html")); err != nil {
		t.Errorf("home page not written: %v", err)
	}
}

// Someone's work is never written over: a folder with anything in it is
// refused, and left exactly as it was.
func TestWriteRefusesAFolderWithSomethingInIt(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := starter.Write(dir)
	if !errors.Is(err, starter.ErrNotEmpty) {
		t.Fatalf("err = %v, want ErrNotEmpty", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the folder was written to: %d entries", len(entries))
	}
	if got, _ := os.ReadFile(mine); string(got) != "mine" {
		t.Errorf("notes.txt was changed: %q", got)
	}
}

func TestWriteRefusesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taken")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := starter.Write(path); err == nil {
		t.Fatal("Write over a file succeeded")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
