package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The whole loop a new person runs: `devdojo new` writes a site, and the same
// binary reads it back with nothing to say.
func TestNewWritesASiteThatLintsClean(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-site")

	stdout, stderr, code := runCLI(t, "", "new", dir)
	if code != 0 {
		t.Fatalf("new: exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"cd " + dir, "devdojo serve"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("new: stdout does not say %q:\n%s", want, stdout)
		}
	}
	for _, file := range []string{"src/pages/index.html", "src/components/layouts/main.html", "public/favicon.svg"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			t.Errorf("new did not write %s: %v", file, err)
		}
	}

	stdout, stderr, code = runCLI(t, "", "lint", dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("lint of a new site: exit %d, stdout %q, stderr %q; want a silent 0", code, stdout, stderr)
	}
}

func TestNewRefusesAFolderWithSomethingInIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "", "new", dir)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "not empty") {
		t.Errorf("stderr does not say the folder is not empty: %s", stderr)
	}
	if got := tree(t, dir); strings.Contains(got, "src") {
		t.Errorf("the folder was written to:\n%s", got)
	}
}

func TestNewUsage(t *testing.T) {
	for _, args := range [][]string{
		{"new"},
		{"new", "one", "two"},
		{"new", "--template", "blog"},
	} {
		_, stderr, code := runCLI(t, "", args...)
		if code != 2 {
			t.Errorf("%v: exit code = %d, want 2", args, code)
		}
		if !strings.Contains(stderr, "usage: devdojo new <folder>") {
			t.Errorf("%v: stderr is not the usage line: %s", args, stderr)
		}
	}
}

func TestUsageNamesNew(t *testing.T) {
	_, stderr, _ := runCLI(t, "")
	if !strings.Contains(stderr, "new <folder>") {
		t.Errorf("the usage text does not name `new <folder>`:\n%s", stderr)
	}
}
