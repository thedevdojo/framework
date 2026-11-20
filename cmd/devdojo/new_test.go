package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/starter/startertest"
)

// templateServer stands in for GitHub: the starter, a blog template and a
// repository of someone else's, each a small site.
func templateServer(t *testing.T) *startertest.Server {
	t.Helper()
	site := startertest.Archive("x-HEAD", startertest.Site())
	return startertest.Serve(t, map[string][]byte{
		"devdojo-templates/starter": site,
		"devdojo-templates/blog":    site,
		"someone/their-site":        site,
		"devdojo-templates/empty":   startertest.Archive("empty-HEAD", map[string]string{"README.md": "# empty"}),
	})
}

// newEnv is the environment that points `devdojo new` at a test server.
func newEnv(s *startertest.Server) []string {
	return []string{"DEVDOJO_TEMPLATES_URL=" + s.URL}
}

// The whole loop a new person runs: `devdojo new` writes a site, and the same
// binary reads it back with nothing to say.
func TestNewWritesASiteThatLintsClean(t *testing.T) {
	server := templateServer(t)
	dir := filepath.Join(t.TempDir(), "my-site")

	stdout, stderr, code := runCLIEnv(t, newEnv(server), "", "new", dir)
	if code != 0 {
		t.Fatalf("new: exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"github.com/devdojo-templates/starter", "cd " + dir, "devdojo serve"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("new: stdout does not say %q:\n%s", want, stdout)
		}
	}
	for _, file := range []string{"src/pages/index.html", "src/components/layouts/main.html", "public/favicon.svg"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			t.Errorf("new did not write %s: %v", file, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "template.json")); err == nil {
		t.Error("new wrote template.json, which only the template's listing needs")
	}

	stdout, stderr, code = runCLI(t, "", "lint", dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("lint of a new site: exit %d, stdout %q, stderr %q; want a silent 0", code, stdout, stderr)
	}
}

// --template names the template three ways, and each is asked for at the
// address GitHub hands that repository out from.
func TestNewTemplateNames(t *testing.T) {
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"--template", "blog"}, "/devdojo-templates/blog/tar.gz/HEAD"},
		{[]string{"--template=someone/their-site"}, "/someone/their-site/tar.gz/HEAD"},
		{[]string{"--template", "https://github.com/someone/their-site"}, "/someone/their-site/tar.gz/HEAD"},
	} {
		server := templateServer(t)
		dir := filepath.Join(t.TempDir(), "my-site")
		args := append([]string{"new", dir}, tc.args...)
		if _, stderr, code := runCLIEnv(t, newEnv(server), "", args...); code != 0 {
			t.Errorf("%v: exit %d (stderr: %s)", tc.args, code, stderr)
			continue
		}
		if got := server.Paths(); len(got) != 1 || got[0] != tc.path {
			t.Errorf("%v: asked for %v, want %s", tc.args, got, tc.path)
		}
	}
}

// A folder with something in it is refused before anything is downloaded.
func TestNewRefusesAFolderWithSomethingInIt(t *testing.T) {
	server := templateServer(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIEnv(t, newEnv(server), "", "new", dir)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "is not empty. Pick a folder that does not exist yet, or an empty one.") {
		t.Errorf("stderr does not say the folder is not empty: %s", stderr)
	}
	if got := tree(t, dir); strings.Contains(got, "src") {
		t.Errorf("the folder was written to:\n%s", got)
	}
	if n := server.Requests.Load(); n != 0 {
		t.Errorf("%d downloads were asked for", n)
	}
}

// What goes wrong is said in a sentence, and nothing is left behind.
func TestNewFailures(t *testing.T) {
	server := templateServer(t)
	for _, tc := range []struct {
		env  []string
		args []string
		want string
	}{
		{newEnv(server), []string{"--template", "nope"}, `there is no template called "nope" at github.com/devdojo-templates/nope`},
		{newEnv(server), []string{"--template", "empty"}, "github.com/devdojo-templates/empty is not a site: it has no src/ or resources/ folder"},
		{[]string{"DEVDOJO_TEMPLATES_URL=http://127.0.0.1:1"}, nil, "couldn't reach GitHub"},
	} {
		parent := t.TempDir()
		args := append([]string{"new", filepath.Join(parent, "my-site")}, tc.args...)
		_, stderr, code := runCLIEnv(t, tc.env, "", args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1", tc.args, code)
		}
		if !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: stderr does not say %q:\n%s", tc.args, tc.want, stderr)
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 0 {
			t.Errorf("%v: left behind %d things in the parent folder", tc.args, len(entries))
		}
	}
}

func TestNewUsage(t *testing.T) {
	for _, args := range [][]string{
		{"new"},
		{"new", "one", "two"},
		{"new", "--template", "blog"},
		{"new", "my-site", "--template"},
		{"new", "my-site", "--force"},
	} {
		_, stderr, code := runCLI(t, "", args...)
		if code != 2 {
			t.Errorf("%v: exit code = %d, want 2", args, code)
		}
		if !strings.Contains(stderr, "usage: devdojo new <folder> [--template <name>]") {
			t.Errorf("%v: stderr is not the usage line: %s", args, stderr)
		}
	}
	_, stderr, code := runCLI(t, "", "new", "my-site", "--template", "https://gitlab.com/a/b")
	if code != 2 || !strings.Contains(stderr, "is not a GitHub repository") {
		t.Errorf("a template that is not on GitHub: exit %d, stderr %s", code, stderr)
	}
}

func TestUsageNamesNew(t *testing.T) {
	_, stderr, _ := runCLI(t, "")
	if !strings.Contains(stderr, "new <folder> [--template <name>]") {
		t.Errorf("the usage text does not name `new <folder> [--template <name>]`:\n%s", stderr)
	}
}
