package framework_test

import (
	"errors"
	"os"
	"testing"

	"github.com/thedevdojo/framework"
	"github.com/thedevdojo/framework/blade/convert"
	"github.com/thedevdojo/framework/vfs"
)

// TestCompileRoundTrip drives the whole engine through the root facade only —
// no reach into blade/render — proving framework.Compile and the aliased
// Options/Result/Diagnostic types are a complete embedding surface.
func TestCompileRoundTrip(t *testing.T) {
	fs := vfs.Mem{
		"src/pages/index.blade.php": "<h1>{{ $site->title }}</h1>",
		"src/data/site.json":        `{"title":"Hello"}`,
	}

	res := framework.Compile(fs, []string{"src/pages/index.blade.php"}, framework.Options{})

	if got := res.Files["index.html"]; got != "<h1>Hello</h1>" {
		t.Fatalf("index.html = %q, want %q", got, "<h1>Hello</h1>")
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if deps := res.Deps["index.html"]; len(deps) == 0 {
		t.Errorf("deps should include the entry, got %v", deps)
	}
	if framework.Version == "" {
		t.Error("Version should be non-empty")
	}
}

// TestFacadeDiagnosticKinds proves the kind constants and Analyze are reachable
// through the facade and carry the render package's own values.
func TestFacadeDiagnosticKinds(t *testing.T) {
	fs := vfs.Mem{
		"src/pages/index.blade.php":     `<x-card title="typo"/>`,
		"src/components/card.blade.php": "@props(['body' => ''])\n<div>{{ $body }}</div>",
	}

	// Compile: an undefined-variable style error would be KindError; here the
	// page is clean, so no diagnostics — but a bad page proves the alias works.
	bad := vfs.Mem{"src/pages/index.blade.php": "{{ $nope }}"}
	res := framework.Compile(bad, []string{"src/pages/index.blade.php"}, framework.Options{})
	if len(res.Diagnostics) == 0 || res.Diagnostics[0].Kind != framework.KindError {
		t.Fatalf("want a KindError diagnostic, got %v", res.Diagnostics)
	}

	// Analyze: the lint-only warning pass, reachable through the facade.
	ws := framework.Analyze(fs, []string{"src/pages/index.blade.php"})
	if len(ws) != 1 || ws[0].Kind != framework.KindWarning {
		t.Fatalf("want one KindWarning from Analyze, got %v", ws)
	}
}

// TestExamplesRenderAlike: example/ and example-studio/ are the same site
// written in the two syntaxes — Blade and DevDojo Studio. They must lint clean and
// render the same bytes, and those bytes are the ones committed in
// example/_out (which real Laravel Blade also produces; see example/README).
func TestExamplesRenderAlike(t *testing.T) {
	render := func(dir, entry string) string {
		t.Helper()
		fs := vfs.Dir(dir)
		res := framework.Compile(fs, []string{entry}, framework.Options{})
		if len(res.Diagnostics) != 0 {
			t.Fatalf("%s: diagnostics = %+v; want none", dir, res.Diagnostics)
		}
		if warnings := framework.Analyze(fs, []string{entry}); len(warnings) != 0 {
			t.Fatalf("%s: lint warnings = %+v; want none", dir, warnings)
		}
		out, ok := res.Files["index.html"]
		if !ok {
			t.Fatalf("%s: no index.html in %v", dir, res.Files)
		}
		return out
	}
	bladeOut := render("example", "src/pages/index.blade.php")
	htmlOut := render("example-studio", "src/pages/index.html")
	if htmlOut != bladeOut {
		t.Errorf("the Studio-syntax example renders differently:\n--- blade ---\n%s\n--- studio ---\n%s", bladeOut, htmlOut)
	}
	// _out/ is render.sh's output and is not in git: on a fresh checkout there
	// is nothing to compare with, and nothing to be stale.
	committed, err := os.ReadFile("example/_out/index.html")
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if bladeOut != string(committed) {
		t.Errorf("example/_out/index.html is stale; re-run example/render.sh")
	}
}

// TestExampleStudioIsTheConversion: example-studio/ is not a hand-kept copy. Its
// views are exactly what `devdojo convert example --to studio` writes, so
// the pair doubles as the converter's worked example.
func TestExampleStudioIsTheConversion(t *testing.T) {
	res := convert.Site(vfs.Dir("example"), convert.Studio, framework.Options{})
	if len(res.Problems) != 0 {
		t.Fatalf("example/ did not convert: %+v", res.Problems)
	}
	if len(res.Files) == 0 {
		t.Fatal("example/ converted to no files")
	}
	for path, got := range res.Files {
		want, err := os.ReadFile("example-studio/" + path)
		if err != nil {
			t.Errorf("example-studio/ is missing %s", path)
			continue
		}
		if got != string(want) {
			t.Errorf("example-studio/%s is not the conversion of example/:\n--- converted ---\n%s\n--- committed ---\n%s", path, got, want)
		}
	}
}
