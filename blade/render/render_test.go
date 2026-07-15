package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/vfs"
)

func TestEscape(t *testing.T) {
	if got := Escape(`<a href="x">&'`); got != `&lt;a href=&quot;x&quot;&gt;&amp;&#039;` {
		t.Fatal(got)
	}
	if got := Escape("bad\xffutf8"); got != "bad�utf8" {
		t.Fatal(got)
	}
}

func TestStringify(t *testing.T) {
	cases := []struct {
		in   expr.Value
		want string
	}{
		{nil, ""}, {false, ""}, {true, "1"},
		{int64(42), "42"}, {1.0, "1"}, {2.5, "2.5"}, {0.1, "0.1"},
	}
	for _, c := range cases {
		got, err := Stringify(c.in)
		if err != nil || got != c.want {
			t.Fatalf("Stringify(%v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := Stringify([]expr.Value{}); err == nil {
		t.Fatal("arrays must error")
	}
}

// TestNewlineModel encodes the oracle-verified newline rule directly (the
// full-file version rides in fixtures/newline-eating). Echoes and comments
// preserve the following newline; block directives eat one.
func TestNewlineModel(t *testing.T) {
	src := "A{{ $x }}\nB\n@if (true)\nC\n@endif\nD{{-- gone --}}\nE\n"
	fs := vfs.Mem{"resources/views/pages/index.blade.html": src}
	res := Compile(fs, []string{"resources/views/pages/index.blade.html"}, Options{
		Data: map[string]any{"x": float64(1)},
	})
	if got := res.Files["index.html"]; got != "A1\nB\nC\nD\nE\n" {
		t.Fatalf("newline model = %q; want %q", got, "A1\nB\nC\nD\nE\n")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if deps := res.Deps["index.html"]; len(deps) != 1 || deps[0] != "resources/views/pages/index.blade.html" {
		t.Fatalf("deps = %v", deps)
	}
}

func TestOutputNaming(t *testing.T) {
	cases := map[string]string{
		"resources/views/pages/about.blade.html":      "about.html",
		"resources/views/pages/index.blade.html":      "index.html",
		"resources/views/pages/docs/intro.blade.html": "docs/intro.html",
		"resources/views/components/card.blade.html":  "components/card.html",
	}
	for entry, want := range cases {
		if got := outputName(entry); got != want {
			t.Errorf("outputName(%q) = %q; want %q", entry, got, want)
		}
	}
}

// TestCompileEntryEscapesRejected: an entry whose cleaned path escapes the site
// root — via a "../" walk that surfaces through the output name, or an absolute
// path — is refused with an error/entry-invalid diagnostic and produces NO
// output file. The render/protocol path must not let a crafted entry read
// outside the tree or write above the destination directory.
func TestCompileEntryEscapesRejected(t *testing.T) {
	cases := []string{
		"resources/views/pages/../../../x.blade.html", // output name would be ../../../x.html
		"/etc/passwd", // absolute entry
	}
	for _, entry := range cases {
		fs := vfs.Mem{"resources/views/pages/index.blade.html": "hi"}
		res := Compile(fs, []string{entry}, Options{})
		if len(res.Files) != 0 {
			t.Errorf("%q: produced output files %v; want none", entry, res.Files)
		}
		if len(res.Diagnostics) != 1 {
			t.Fatalf("%q: diagnostics = %v; want exactly one", entry, res.Diagnostics)
		}
		d := res.Diagnostics[0]
		if d.Kind != KindError || d.Construct != "entry-invalid" {
			t.Errorf("%q: diag = %+v; want error/entry-invalid", entry, d)
		}
		if d.File != entry || d.Line != 1 || d.Col != 1 {
			t.Errorf("%q: diag pos = %s:%d:%d; want %s:1:1", entry, d.File, d.Line, d.Col, entry)
		}
	}
}

// TestCompileViteCannotEscapeRoot: @vite(['../../secret.css']) resolved against
// a vfs.Dir must not read a file outside the site root — the guarded Dir.Read
// returns not-found, so the secret never lands in the output and a @vite
// not-found diagnostic fires instead. This exercises the guard through the real
// render path (a component/@vite fs read), not just vfs in isolation.
func TestCompileViteCannotEscapeRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "a", "b") // root two levels under base
	pages := filepath.Join(root, "resources", "views", "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "index.blade.html"),
		[]byte(`@vite(['../../secret.css'])`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A secret exactly where "../../secret.css" would resolve from root.
	if err := os.WriteFile(filepath.Join(base, "secret.css"),
		[]byte("SECRETCSS"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Compile(vfs.Dir(root), []string{"resources/views/pages/index.blade.html"}, Options{})
	if out := res.Files["index.html"]; strings.Contains(out, "SECRETCSS") {
		t.Fatalf("output leaked a file outside the root: %q", out)
	}
	if !hasConstruct(res.Diagnostics, "@vite") {
		t.Fatalf("diagnostics = %v; want a @vite not-found", res.Diagnostics)
	}
}

// TestEchoDiagnostics covers the three echo error classes and asserts the
// diagnostic kind, construct, empty output, and that file/line/col ride from
// the node position.
func TestEchoDiagnostics(t *testing.T) {
	cases := []struct {
		src           string
		data          map[string]any
		wantKind      DiagKind
		wantConstruct string
	}{
		{"{{ $missing }}", nil, KindError, "undefined-variable"},
		{"{{ $a + $b }}", map[string]any{"a": float64(1), "b": float64(2)}, KindAppTier, "expression"},
		{"{{ ]] }}", nil, KindError, "expression"},
		{"{{ $arr }}", map[string]any{"arr": []any{float64(1)}}, KindError, "expression"},
	}
	for _, c := range cases {
		entry := "resources/views/pages/index.blade.html"
		fs := vfs.Mem{entry: "X" + c.src + "Y"}
		res := Compile(fs, []string{entry}, Options{Data: c.data})
		if got := res.Files["index.html"]; got != "XY" {
			t.Errorf("%q: output = %q; want %q", c.src, got, "XY")
		}
		if len(res.Diagnostics) != 1 {
			t.Fatalf("%q: want 1 diagnostic, got %v", c.src, res.Diagnostics)
		}
		d := res.Diagnostics[0]
		if d.Kind != c.wantKind || d.Construct != c.wantConstruct {
			t.Errorf("%q: diag kind/construct = %q/%q; want %q/%q", c.src, d.Kind, d.Construct, c.wantKind, c.wantConstruct)
		}
		if d.File != entry || d.Line != 1 || d.Col != 2 {
			t.Errorf("%q: diag pos = %s:%d:%d; want %s:1:2", c.src, d.File, d.Line, d.Col, entry)
		}
	}
}

// TestIfConditionError: a condition that fails to evaluate diagnoses and the
// branch is treated false.
func TestIfConditionError(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	fs := vfs.Mem{entry: "@if ($missing)yes @else no @endif"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != " no " {
		t.Fatalf("output = %q; want %q", got, " no ")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Construct != "undefined-variable" {
		t.Fatalf("diagnostics = %v", res.Diagnostics)
	}
}

// TestUnknownDirectiveRendersLiteral: an Unknown directive (any directive
// outside the closed Blade subset, e.g. @csrf) renders its byte-faithful source
// LITERALLY — matching real Blade for unregistered directives — and raises
// exactly one app-tier diagnostic naming the construct ("@csrf"). See
// diagnostics_test.go for the fuller Unknown/$attributes/parse-diag/
// entry-missing/dedupe coverage. Surrounding literal text is preserved.
// (Foreach is rendered — see control_test.go; Component is rendered — see
// component_test.go; Vite is rendered — see vite_test.go.)
func TestUnknownDirectiveRendersLiteral(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	// '>' (non-word) before @csrf gives it the word boundary real Blade's \B@
	// rule requires, keeping the directive at column 2.
	src := ">@csrf.done"
	fs := vfs.Mem{entry: src}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != ">@csrf.done" {
		t.Fatalf("output = %q; want %q", got, ">@csrf.done")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "@csrf" {
		t.Fatalf("diag = %+v; want app-tier/@csrf", d)
	}
	if d.File != entry || d.Line != 1 || d.Col != 2 {
		t.Fatalf("diag pos = %s:%d:%d; want %s:1:2", d.File, d.Line, d.Col, entry)
	}
}

// TestCompileNeverPanics runs the scanner/parser fuzz seeds through Compile as
// a single view — Compile must always return, never panic, on garbage.
func TestCompileNeverPanics(t *testing.T) {
	seeds := []string{
		"{{ $x }}", "@if($a)x@endif", "<x-a b='c'>", "@{{", "{{--",
		"@foreach(", "</x-slot>", "<x-slot:head class='p-4'>", "@@if",
		"{!! $h !!}", "line1\n{{ $x }}\n<x-btn label=\"a > b\"/>", "@if ($a == 'x)')",
		"@if ($a)A@elseif ($b)B@else C@endif",
		"@foreach ($rows as $row){{ $row }}@endforeach",
		"@foreach ($rows as $k => $v)@break($v)@continue@endforeach",
		"<x-layouts.main title=\"Home\"><x-slot:head><meta></x-slot>body</x-layouts.main>",
		"<x-a><x-a><x-a>x</x-a></x-a></x-a>",
		"@endif", "</x-card>", "@foreach ($rows)x@endforeach", "@foreach ( as $r)@endforeach",
		"<x-c><x-slot:a>1</x-slot><x-slot:a>2</x-slot></x-c>",
		"@if ($a)@foreach ($x as $y)@else@endforeach@endif",
		"@auth secret @endauth", "@props(['title' => 'x'])@vite(['a.js'])",
		"{{-- c --}}{{ $x }}{!! $h !!}", "@if (@foreach (<x-",
		"@foreach ($a as $b => $c => $d)@endforeach", "",
		"{{ }}", "{{ ]]] }}", "{!! !!}", "@if()@endif", "{{ $a->b->c }}",
	}
	entry := "resources/views/pages/index.blade.html"
	for _, s := range seeds {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Compile panicked on %q: %v", s, r)
				}
			}()
			_ = Compile(vfs.Mem{entry: s}, []string{entry}, Options{})
		}()
	}
}
