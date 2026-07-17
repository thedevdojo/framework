package pocketknife_test

import (
	"testing"

	"github.com/thedevdojo/pocketknife"
	"github.com/thedevdojo/pocketknife/vfs"
)

// TestCompileRoundTrip drives the whole engine through the root facade only —
// no reach into blade/render — proving pocketknife.Compile and the aliased
// Options/Result/Diagnostic types are a complete embedding surface.
func TestCompileRoundTrip(t *testing.T) {
	fs := vfs.Mem{
		"resources/views/pages/index.blade.php": "<h1>{{ $site->title }}</h1>",
		"resources/data/site.json":              `{"title":"Hello"}`,
	}

	res := pocketknife.Compile(fs, []string{"resources/views/pages/index.blade.php"}, pocketknife.Options{})

	if got := res.Files["index.html"]; got != "<h1>Hello</h1>" {
		t.Fatalf("index.html = %q, want %q", got, "<h1>Hello</h1>")
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if deps := res.Deps["index.html"]; len(deps) == 0 {
		t.Errorf("deps should include the entry, got %v", deps)
	}
	if pocketknife.Version == "" {
		t.Error("Version should be non-empty")
	}
}

// TestFacadeDiagnosticKinds proves the kind constants and Analyze are reachable
// through the facade and carry the render package's own values.
func TestFacadeDiagnosticKinds(t *testing.T) {
	fs := vfs.Mem{
		"resources/views/pages/index.blade.php":     `<x-card title="typo"/>`,
		"resources/views/components/card.blade.php": "@props(['body' => ''])\n<div>{{ $body }}</div>",
	}

	// Compile: an undefined-variable style error would be KindError; here the
	// page is clean, so no diagnostics — but a bad page proves the alias works.
	bad := vfs.Mem{"resources/views/pages/index.blade.php": "{{ $nope }}"}
	res := pocketknife.Compile(bad, []string{"resources/views/pages/index.blade.php"}, pocketknife.Options{})
	if len(res.Diagnostics) == 0 || res.Diagnostics[0].Kind != pocketknife.KindError {
		t.Fatalf("want a KindError diagnostic, got %v", res.Diagnostics)
	}

	// Analyze: the lint-only warning pass, reachable through the facade.
	ws := pocketknife.Analyze(fs, []string{"resources/views/pages/index.blade.php"})
	if len(ws) != 1 || ws[0].Kind != pocketknife.KindWarning {
		t.Fatalf("want one KindWarning from Analyze, got %v", ws)
	}
}
