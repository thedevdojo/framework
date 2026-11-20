package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/expr"
	"github.com/thedevdojo/framework/vfs"
)

func TestEscape(t *testing.T) {
	if got := Escape(`<a href="x">&'`); got != `&lt;a href=&quot;x&quot;&gt;&amp;&#039;` {
		t.Fatal(got)
	}
	if got := Escape("bad\xffutf8"); got != "bad�utf8" {
		t.Fatal(got)
	}
}

// TestEscapeInvalidUTF8 pins Escape's U+FFFD substitution to PHP's
// htmlspecialchars(…, ENT_SUBSTITUTE, 'UTF-8') exactly (oracle-verified — see
// the escape-invalid-utf8 fixture). The load-bearing rule: an ill-formed multi-
// byte subsequence collapses to a SINGLE U+FFFD (the lead plus its continuation
// bytes), not one per byte.
func TestEscapeInvalidUTF8(t *testing.T) {
	const fffd = "�"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lone-ff", "a\xffb", "a" + fffd + "b"},
		{"lead-then-non-continuation", "a\xc3\x28b", "a" + fffd + "(b"},
		{"truncated-3byte-collapses-to-one", "a\xe2\x82b", "a" + fffd + "b"},
		{"lone-continuation", "a\x80b", "a" + fffd + "b"},
		{"overlong-e0-still-one", "a\xe0\x80\x80b", "a" + fffd + "b"},
		{"surrogate-ed-still-one", "a\xed\xbf\xbfb", "a" + fffd + "b"},
		{"four-byte-lead-consumes-three", "a\xf0\x90\x80" + "Ab", "a" + fffd + "Ab"},
		{"two-lone-ff-are-two", "a\xff\xffb", "a" + fffd + fffd + "b"},
		{"c0-is-invalid-lead-not-two-byte", "a\xc0\x80b", "a" + fffd + fffd + "b"},
		{"f5-is-invalid-lead", "a\xf5\x80\x80\x80b", "a" + fffd + fffd + fffd + fffd + "b"},
		// Trailing-invalid-lead absorption: an early-stopping run absorbs ONE
		// terminating 0xC0/0xC1/0xF5–0xFF byte into the same U+FFFD.
		{"absorbs-trailing-ff", "a\xc3\xffb", "a" + fffd + "b"},
		{"absorbs-trailing-c0", "a\xc3\xc0b", "a" + fffd + "b"},
		{"absorbs-trailing-c1", "a\xc3\xc1b", "a" + fffd + "b"},
		{"absorbs-trailing-f5", "a\xc3\xf5b", "a" + fffd + "b"},
		{"absorbs-after-two-continuations", "a\xe2\x82\xc0b", "a" + fffd + "b"},
		{"absorbs-ff-after-two-continuations", "a\xe2\x82\xffb", "a" + fffd + "b"},
		{"absorbs-ff-after-three-continuations", "a\xf0\x90\x80\xffb", "a" + fffd + "b"},
		{"valid-lead-terminator-not-absorbed", "a\xc3\xc2\xa9b", "a" + fffd + "\xc2\xa9b"},
		{"absorption-does-not-chain", "a\xc3\xff\xffb", "a" + fffd + fffd + "b"},
		{"absorbed-byte-not-a-new-lead", "a\xc3\xc0\x80b", "a" + fffd + fffd + "b"},
		{"no-absorption-after-full-run", "a\xe0\x80\x80\xffb", "a" + fffd + fffd + "b"},
		{"absorbs-mid-run-stop", "a\xe0\x80\xffb", "a" + fffd + "b"},
		{"valid-utf8-preserved", "a\xc3\xa9b", "a\xc3\xa9b"},
		{"valid-euro-preserved", "a\xe2\x82\xacb", "a\xe2\x82\xacb"},
	}
	for _, c := range cases {
		if got := Escape(c.in); got != c.want {
			t.Errorf("%s: Escape(% x) = % x; want % x", c.name, c.in, got, c.want)
		}
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
	// An object cannot be echoed either — PHP fatals casting stdClass to string
	// (htmlspecialchars(): Argument #1 must be of type string, stdClass given;
	// oracle-probed). Empty or not, Stringify refuses it.
	if _, err := Stringify(expr.NewObject()); err == nil {
		t.Fatal("objects must error")
	}
	obj := expr.NewObject()
	obj.Set("a", int64(1))
	if _, err := Stringify(obj); err == nil {
		t.Fatal("non-empty objects must error")
	}
}

// TestEchoObjectDiagnoses: echoing an object ({{ $site }}) renders empty with an
// error/expression diagnostic — the render-tier surface of Stringify's refusal.
func TestEchoObjectDiagnoses(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{
		entry:                      "X{{ $site }}Y",
		"resources/data/site.json": `{"name":"Acme"}`,
	}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "XY" {
		t.Fatalf("output = %q; want %q", got, "XY")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Kind != KindError || res.Diagnostics[0].Construct != "expression" {
		t.Fatalf("diagnostics = %v; want one error/expression", res.Diagnostics)
	}
}

// TestNewlineModel encodes the oracle-verified newline rule directly (the
// full-file version rides in fixtures/newline-eating). Echoes and comments
// preserve the following newline; block directives eat one.
func TestNewlineModel(t *testing.T) {
	src := "A{{ $x }}\nB\n@if (true)\nC\n@endif\nD{{-- gone --}}\nE\n"
	fs := vfs.Mem{"resources/views/pages/index.blade.php": src}
	res := Compile(fs, []string{"resources/views/pages/index.blade.php"}, Options{
		Data: map[string]any{"x": float64(1)},
	})
	if got := res.Files["index.html"]; got != "A1\nB\nC\nD\nE\n" {
		t.Fatalf("newline model = %q; want %q", got, "A1\nB\nC\nD\nE\n")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if deps := res.Deps["index.html"]; len(deps) != 1 || deps[0] != "resources/views/pages/index.blade.php" {
		t.Fatalf("deps = %v", deps)
	}
}

func TestOutputNaming(t *testing.T) {
	cases := map[string]string{
		"resources/views/pages/about.blade.php":      "about.html",
		"resources/views/pages/index.blade.php":      "index.html",
		"resources/views/pages/docs/intro.blade.php": "docs/intro.html",
		"resources/views/components/card.blade.php":  "components/card.html",
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
		"resources/views/pages/../../../x.blade.php", // output name would be ../../../x.html
		"/etc/passwd", // absolute entry
	}
	for _, entry := range cases {
		fs := vfs.Mem{"resources/views/pages/index.blade.php": "hi"}
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
	if err := os.WriteFile(filepath.Join(pages, "index.blade.php"),
		[]byte(`@vite(['../../secret.css'])`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A secret exactly where "../../secret.css" would resolve from root.
	if err := os.WriteFile(filepath.Join(base, "secret.css"),
		[]byte("SECRETCSS"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Compile(vfs.Dir(root), []string{"resources/views/pages/index.blade.php"}, Options{})
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
		entry := "resources/views/pages/index.blade.php"
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
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: "@if ($missing)yes @else no @endif"}
	res := Compile(fs, []string{entry}, Options{})
	// The page output's leading space is stripped by the entry-level ltrim (real
	// Blade's PhpEngine::evaluatePath returns ltrim(ob_get_clean())); the trailing
	// space is preserved.
	if got := res.Files["index.html"]; got != "no " {
		t.Fatalf("output = %q; want %q", got, "no ")
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
	entry := "resources/views/pages/index.blade.php"
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
	entry := "resources/views/pages/index.blade.php"
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
