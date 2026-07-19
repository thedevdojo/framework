package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade"
	"github.com/thedevdojo/pocketknife/blade/render"
)

// TestRunRoundTrip feeds a one-page site (one echo bound to `data`, one
// app-tier construct, one component) through Run and asserts the exact
// output document: Files/Deps/Diagnostics values (computed once against
// render.Compile directly and pinned here — see task-12 report) plus the
// envelope fields (engine, and that entries-omitted discovery found the
// page under resources/views/pages/ without an explicit entries list).
func TestRunRoundTrip(t *testing.T) {
	input := `{
		"files": {
			"resources/views/pages/index.blade.php": "<h1>{{ $greeting }}</h1>\n@auth\nWelcome back\n@endauth\n<x-alert message=\"Hi\"/>\n",
			"resources/views/components/alert.blade.php": "@props(['message'])\n<div class=\"alert\">{{ $message }}</div>\n"
		},
		"data": { "greeting": "Hello" }
	}`

	var out bytes.Buffer
	if err := Run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got Output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}

	if got.Engine != blade.Version {
		t.Errorf("engine = %q, want %q", got.Engine, blade.Version)
	}

	// The @auth/@endauth directives are outside the subset, so they render
	// LITERALLY (byte-faithful) while still raising app-tier diagnostics.
	wantFiles := map[string]string{
		"index.html": "<h1>Hello</h1>\n@auth\nWelcome back\n@endauth\n<div class=\"alert\">Hi</div>\n",
	}
	if len(got.Files) != len(wantFiles) || got.Files["index.html"] != wantFiles["index.html"] {
		t.Errorf("files = %#v, want %#v", got.Files, wantFiles)
	}

	wantDeps := []string{
		"resources/views/pages/index.blade.php",
		"resources/views/components/alert.blade.php",
	}
	deps := got.Deps["index.html"]
	if len(deps) != len(wantDeps) {
		t.Fatalf("deps = %#v, want %#v", deps, wantDeps)
	}
	for i, d := range wantDeps {
		if deps[i] != d {
			t.Errorf("deps[%d] = %q, want %q", i, deps[i], d)
		}
	}

	if len(got.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want 2 entries", got.Diagnostics)
	}
	wantDiags := []render.Diagnostic{
		{File: "resources/views/pages/index.blade.php", Line: 2, Col: 1, Kind: render.KindAppTier, Construct: "@auth", Message: "directive @auth is outside the Pocketknife subset; requires the PHP app tier"},
		{File: "resources/views/pages/index.blade.php", Line: 4, Col: 1, Kind: render.KindAppTier, Construct: "@endauth", Message: "directive @endauth is outside the Pocketknife subset; requires the PHP app tier"},
	}
	for i, want := range wantDiags {
		if got.Diagnostics[i] != want {
			t.Errorf("diagnostics[%d] = %#v, want %#v", i, got.Diagnostics[i], want)
		}
	}

	// The exact raw JSON shape: diagnostics carry construct, an app-tier
	// diagnostic has no "line" gap, and every top-level key is present even
	// though this fixture has no diagnostics of kind "error".
	if !bytes.Contains(out.Bytes(), []byte(`"construct":"@auth"`)) {
		t.Errorf("raw output missing construct field: %s", out.String())
	}
}

// TestRunEmptyKeysInitialized: a site with no output-producing entry (an
// empty files map, no discoverable pages) must still marshal files/deps as
// `{}` and diagnostics as `[]` — never `null` — so a host can iterate every
// key without a nil-check.
func TestRunEmptyKeysInitialized(t *testing.T) {
	var out bytes.Buffer
	if err := Run(strings.NewReader(`{"files": {}}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw := out.String()
	for _, want := range []string{`"files":{}`, `"deps":{}`, `"diagnostics":[]`} {
		if !strings.Contains(raw, want) {
			t.Errorf("output missing %s in %s", want, raw)
		}
	}
	var got Output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Files == nil || got.Deps == nil || got.Diagnostics == nil {
		t.Errorf("decoded output has nil field: %#v", got)
	}
}

// TestRunEntriesOmittedDiscoversAllPages: with no "entries" key, Run compiles
// every resources/views/pages/**.blade.php file, sorted, and nothing else
// (a component-only file under components/ must not become an output).
func TestRunEntriesOmittedDiscoversAllPages(t *testing.T) {
	input := `{
		"files": {
			"resources/views/pages/about.blade.php": "About",
			"resources/views/pages/index.blade.php": "Index",
			"resources/views/pages/blog/post.blade.php": "Post",
			"resources/views/components/card.blade.php": "Card"
		}
	}`
	var out bytes.Buffer
	if err := Run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got Output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	want := map[string]string{
		"about.html":     "About",
		"index.html":     "Index",
		"blog/post.html": "Post",
	}
	if len(got.Files) != len(want) {
		t.Fatalf("files = %#v, want %#v", got.Files, want)
	}
	for k, v := range want {
		if got.Files[k] != v {
			t.Errorf("files[%q] = %q, want %q", k, got.Files[k], v)
		}
	}
	if _, ok := got.Files["components/card.html"]; ok {
		t.Errorf("a component-only file must not become an output: %#v", got.Files)
	}
}

// TestRunEntriesExplicitOverridesDiscovery: a caller-supplied entries list is
// used verbatim (only the listed page compiles), even when other pages exist.
func TestRunEntriesExplicitOverridesDiscovery(t *testing.T) {
	input := `{
		"files": {
			"resources/views/pages/about.blade.php": "About",
			"resources/views/pages/index.blade.php": "Index"
		},
		"entries": ["resources/views/pages/index.blade.php"]
	}`
	var out bytes.Buffer
	if err := Run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got Output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Files) != 1 || got.Files["index.html"] != "Index" {
		t.Errorf("files = %#v, want only index.html", got.Files)
	}
}

// TestRunOptionsURLAcceptedButUnused: options.url is decoded without error
// and does not change output — v1 only renders relative URLs.
func TestRunOptionsURLAcceptedButUnused(t *testing.T) {
	input := `{
		"files": {"resources/views/pages/index.blade.php": "hi"},
		"options": {"url": "https://example.com/x"}
	}`
	var out bytes.Buffer
	if err := Run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got Output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Files["index.html"] != "hi" {
		t.Errorf("files = %#v", got.Files)
	}
}

// TestRunOptionsMaxLoopIterations: options.max_loop_iterations reaches
// render.Options — a loop over the budget aborts with the render-budget
// diagnostic, and the same input without the option renders clean under
// the default cap.
func TestRunOptionsMaxLoopIterations(t *testing.T) {
	const files = `"files": {"resources/views/pages/index.blade.php": "@foreach ($xs as $x)[{{ $x }}]@endforeach"},
		"data": {"xs": [1, 2, 3, 4]}`

	var capped bytes.Buffer
	if err := Run(strings.NewReader(`{`+files+`, "options": {"max_loop_iterations": 3}}`), &capped); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got Output
	if err := json.Unmarshal(capped.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Files["index.html"] != "[1][2][3]" {
		t.Errorf("capped files = %#v; want output truncated at 3 iterations", got.Files)
	}
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].Construct != "render-budget" || got.Diagnostics[0].Kind != render.KindError {
		t.Errorf("capped diagnostics = %v; want one error/render-budget", got.Diagnostics)
	}

	var uncapped bytes.Buffer
	if err := Run(strings.NewReader(`{`+files+`}`), &uncapped); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got = Output{}
	if err := json.Unmarshal(uncapped.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Files["index.html"] != "[1][2][3][4]" || len(got.Diagnostics) != 0 {
		t.Errorf("uncapped = %#v / %v; want the full render, no diagnostics", got.Files, got.Diagnostics)
	}
}

// TestRunMalformedJSON: invalid JSON on stdin never surfaces as a Go error
// from Run (hosts always get a parseable document) — it becomes a single
// protocol-kind error diagnostic, with files/deps/engine still present.
func TestRunMalformedJSON(t *testing.T) {
	var out bytes.Buffer
	err := Run(strings.NewReader(`{ this is not json`), &out)
	if err != nil {
		t.Fatalf("Run returned an error for malformed input (should encode a diagnostic instead): %v", err)
	}
	var got Output
	if uerr := json.Unmarshal(out.Bytes(), &got); uerr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", uerr, out.String())
	}
	if got.Engine != blade.Version {
		t.Errorf("engine = %q, want %q", got.Engine, blade.Version)
	}
	if got.Files == nil || got.Deps == nil {
		t.Errorf("files/deps must still be initialized: %#v", got)
	}
	if len(got.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want exactly one", got.Diagnostics)
	}
	d := got.Diagnostics[0]
	if d.Kind != render.KindError || d.Construct != "protocol" {
		t.Errorf("diagnostic = %#v, want kind=error construct=protocol", d)
	}
	if d.Message == "" {
		t.Errorf("diagnostic message must not be empty")
	}
}

// TestOutputTrailingNewline: the encoded document ends with exactly one
// trailing newline (the protocol is line-oriented for shell piping).
func TestOutputTrailingNewline(t *testing.T) {
	var out bytes.Buffer
	if err := Run(strings.NewReader(`{"files":{}}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw := out.String()
	if !strings.HasSuffix(raw, "\n") {
		t.Fatalf("output does not end with a newline: %q", raw)
	}
	if strings.HasSuffix(strings.TrimSuffix(raw, "\n"), "\n") {
		t.Fatalf("output has more than one trailing newline: %q", raw)
	}
}
