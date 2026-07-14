package render

import "testing"

// TestParseStringArray drives @vite's argument grammar: a literal array of
// quoted strings, no keys — reusing the same top-level splitter and
// string-literal parser @props uses (TestParseProps, component_test.go), with
// malformed rows reported as ok=false.
func TestParseStringArray(t *testing.T) {
	cases := []struct {
		raw  string
		ok   bool
		want []string
	}{
		{`['resources/css/site.css']`, true, []string{"resources/css/site.css"}},
		{`['resources/css/a.css', 'resources/css/b.css']`, true, []string{"resources/css/a.css", "resources/css/b.css"}},
		{`["resources/css/a.css"]`, true, []string{"resources/css/a.css"}},
		{`[]`, true, nil},
		{`['a.css', 'b.css',]`, true, []string{"a.css", "b.css"}}, // trailing comma tolerated

		// malformed rows → ok=false
		{`'resources/css/a.css'`, false, nil}, // no surrounding brackets
		{`['a.css' => 'x']`, false, nil},      // keyed entry not allowed here
		{`[42]`, false, nil},                  // non-string entry
		{`['a.css' 'b.css']`, false, nil},     // missing comma
		{``, false, nil},                      // empty raw (e.g. @vite())
		{`not-an-array`, false, nil},
	}
	for _, c := range cases {
		got, ok := parseStringArray(c.raw)
		if ok != c.ok {
			t.Errorf("parseStringArray(%q) ok = %v; want %v", c.raw, ok, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("parseStringArray(%q) = %v; want %v", c.raw, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("parseStringArray(%q) = %v; want %v", c.raw, got, c.want)
			}
		}
	}
}

// TestViteSingleCssEntry: the canonical shape — one CDN script, one inlined
// <style>, content verbatim (including the file's own trailing newline —
// settled empirically against the oracle: neither side trims).
func TestViteSingleCssEntry(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                `A@vite(['resources/css/site.css'])B`,
		"resources/css/site.css": "@import \"tailwindcss\";\n.btn{color:red}\n",
	}, nil)
	want := `A<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>` +
		"\n" + `<style type="text/tailwindcss">@import "tailwindcss";` + "\n" + `.btn{color:red}` + "\n" + `</style>B`
	if got := res.Files["index.html"]; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestViteMultipleCssEntriesCdnOnce: the CDN script appears exactly once no
// matter how many CSS entries are given; one <style> per entry, in argument
// order.
func TestViteMultipleCssEntriesCdnOnce(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:             `@vite(['resources/css/a.css', 'resources/css/b.css'])`,
		"resources/css/a.css": "A{}",
		"resources/css/b.css": "B{}",
	}, nil)
	want := `<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>` +
		"\n" + `<style type="text/tailwindcss">A{}</style>` +
		"\n" + `<style type="text/tailwindcss">B{}</style>`
	if got := res.Files["index.html"]; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
}

// TestViteJsEntrySkipped: a .js entry raises an app-tier diagnostic and is
// skipped; with no other entries, nothing renders (not even the CDN script —
// there is nothing to serve it for).
func TestViteJsEntrySkipped(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `A@vite(['resources/js/app.js'])B`,
	}, nil)
	if got := res.Files["index.html"]; got != "AB" {
		t.Fatalf("output = %q; want %q", got, "AB")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "@vite-js" {
		t.Fatalf("diag = %+v; want app-tier/@vite-js", d)
	}
}

// TestViteMissingCssFile: a missing CSS file raises an error diagnostic and
// its style block is omitted; with no other entries, nothing renders.
func TestViteMissingCssFile(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `A@vite(['resources/css/gone.css'])B`,
	}, nil)
	if got := res.Files["index.html"]; got != "AB" {
		t.Fatalf("output = %q; want %q", got, "AB")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "@vite" {
		t.Fatalf("diag = %+v; want error/@vite", d)
	}
}

// TestViteMalformedArgs: an argument that isn't a literal string array (or a
// bare string literal) raises an error diagnostic and nothing renders.
func TestViteMalformedArgs(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `A@vite($css)B`,
	}, nil)
	if got := res.Files["index.html"]; got != "AB" {
		t.Fatalf("output = %q; want %q", got, "AB")
	}
	if !hasConstruct(res.Diagnostics, "@vite") {
		t.Fatalf("diagnostics = %v; want a @vite error", res.Diagnostics)
	}
}

// TestParseViteEntries drives @vite's full argument grammar: the array form
// (delegated to parseStringArray), the idiomatic bare-string form
// (@vite('x.css') — real Blade accepts it, and StaticVite normalizes it on
// the PHP side, so Go must too for engine parity), and the empty form
// (@vite() / bare @vite → no entries, valid, renders nothing).
func TestParseViteEntries(t *testing.T) {
	cases := []struct {
		raw  string
		ok   bool
		want []string
	}{
		{`'resources/css/site.css'`, true, []string{"resources/css/site.css"}},
		{`"resources/css/site.css"`, true, []string{"resources/css/site.css"}},
		{`  'a.css'  `, true, []string{"a.css"}}, // whitespace-padded bare string
		{``, true, nil},                          // @vite / @vite(): no entries
		{`   `, true, nil},
		{`['a.css', 'b.css']`, true, []string{"a.css", "b.css"}}, // array form still works

		// malformed rows → ok=false
		{`$css`, false, nil},
		{`['a' => 'b']`, false, nil},
		{`'unterminated`, false, nil},
		{`42`, false, nil},
	}
	for _, c := range cases {
		got, ok := parseViteEntries(c.raw)
		if ok != c.ok {
			t.Errorf("parseViteEntries(%q) ok = %v; want %v", c.raw, ok, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("parseViteEntries(%q) = %v; want %v", c.raw, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("parseViteEntries(%q) = %v; want %v", c.raw, got, c.want)
			}
		}
	}
}

// TestViteBareStringEntry: @vite('x.css') — no brackets — renders exactly like
// @vite(['x.css']). Idiomatic Blade; StaticVite normalizes the same way.
func TestViteBareStringEntry(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:             `A@vite('resources/css/a.css')B`,
		"resources/css/a.css": "A{}",
	}, nil)
	want := `A<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>` +
		"\n" + `<style type="text/tailwindcss">A{}</style>B`
	if got := res.Files["index.html"]; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestViteEmptyArgsRendersNothing: @vite() and bare @vite have no entries —
// nothing renders (no CDN script either) and no diagnostic is raised,
// matching StaticVite's null→[] normalization (zero entries → HtmlString(”)).
func TestViteEmptyArgsRendersNothing(t *testing.T) {
	for _, src := range []string{"A@vite()B", "A@vite B"} {
		res := compileMem(t, map[string]string{pageEntry: src}, nil)
		want := "AB"
		if src == "A@vite B" {
			want = "A B" // no parens: the trailing space stays literal text
		}
		if got := res.Files["index.html"]; got != want {
			t.Fatalf("src %q: output = %q; want %q", src, got, want)
		}
		if len(res.Diagnostics) != 0 {
			t.Fatalf("src %q: unexpected diagnostics: %v", src, res.Diagnostics)
		}
	}
}

// TestViteMixedEntries: css + js + missing in one call — only the resolved
// CSS entries render, in their original relative order, and both the js-skip
// and missing-file diagnostics are raised.
func TestViteMixedEntries(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:             `@vite(['resources/css/a.css', 'resources/js/app.js', 'resources/css/b.css', 'resources/css/gone.css'])`,
		"resources/css/a.css": "A{}",
		"resources/css/b.css": "B{}",
	}, nil)
	want := `<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>` +
		"\n" + `<style type="text/tailwindcss">A{}</style>` +
		"\n" + `<style type="text/tailwindcss">B{}</style>`
	if got := res.Files["index.html"]; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if !hasConstruct(res.Diagnostics, "@vite-js") || !hasConstruct(res.Diagnostics, "@vite") {
		t.Fatalf("diagnostics = %v; want both @vite-js and @vite", res.Diagnostics)
	}
}

// TestViteDeps: a successfully inlined CSS file joins the output's deps.
func TestViteDeps(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:             `@vite(['resources/css/a.css', 'resources/css/gone.css'])`,
		"resources/css/a.css": "A{}",
	}, nil)
	deps := res.Deps["index.html"]
	want := []string{pageEntry, "resources/css/a.css"}
	if len(deps) != len(want) {
		t.Fatalf("deps = %v; want %v", deps, want)
	}
	for i := range want {
		if deps[i] != want[i] {
			t.Fatalf("deps = %v; want %v", deps, want)
		}
	}
}

// TestViteEatsNewline: @vite compiles like any other close-tag-shaped
// directive (`<?php echo app(Vite::class)(...); ?>` — see
// CompilesHelpers::compileVite), so it eats one following newline exactly
// like @props/a component tag — settled empirically against the oracle
// (task-10-report.md). The eat happens even when nothing renders (no valid
// CSS entries), matching Component/Props's unconditional eat.
func TestViteEatsNewline(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:             "A\n@vite(['resources/css/a.css'])\nB",
		"resources/css/a.css": "A{}",
	}, nil)
	want := "A\n<script src=\"https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4\"></script>\n<style type=\"text/tailwindcss\">A{}</style>B"
	if got := res.Files["index.html"]; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}

	res2 := compileMem(t, map[string]string{
		pageEntry: "A\n@vite(['resources/js/app.js'])\nB",
	}, nil)
	if got := res2.Files["index.html"]; got != "A\nB" {
		t.Fatalf("output = %q; want %q", got, "A\nB")
	}
}
