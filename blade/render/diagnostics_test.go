package render

import (
	"testing"

	"github.com/thedevdojo/framework/blade/parser"
	"github.com/thedevdojo/framework/vfs"
)

// TestUnknownDirectiveAppTier: an ast.Unknown directive (any directive outside
// the closed subset — @auth, @php, @include, @extends, @section, @csrf, …)
// renders LITERALLY (its byte-faithful source, matching real Blade's handling
// of unregistered directives) and raises exactly one app-tier diagnostic whose
// Construct is "@"+Name, at the directive's own position.
func TestUnknownDirectiveAppTier(t *testing.T) {
	entry := "src/pages/index.blade.php"
	// A '>' (non-word) before @auth gives it the word boundary real Blade's \B@
	// rule requires, and keeps the directive at column 2. A space (not "XauthY")
	// after the name: the scanner's directive reader is alnum-greedy, so a letter
	// glued directly onto the name would become part of it ("authy").
	fs := vfs.Mem{entry: ">@auth Y"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != ">@auth Y" {
		t.Fatalf("output = %q; want %q", got, ">@auth Y")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "@auth" {
		t.Fatalf("diag = %+v; want app-tier/@auth", d)
	}
	if d.File != entry || d.Line != 1 || d.Col != 2 {
		t.Fatalf("diag pos = %s:%d:%d; want %s:1:2", d.File, d.Line, d.Col, entry)
	}
}

// TestUnknownDirectiveRendersLiteralWithArgs: an unregistered directive with
// parenthesized args renders byte-identically to its source (spacing and casing
// preserved) and raises one app-tier diagnostic.
func TestUnknownDirectiveRendersLiteralWithArgs(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "A @media (x) B"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "A @media (x) B" {
		t.Fatalf("output = %q; want %q (byte-identical to input)", got, "A @media (x) B")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	if d := res.Diagnostics[0]; d.Kind != KindAppTier || d.Construct != "@media" {
		t.Fatalf("diag = %+v; want app-tier/@media", d)
	}
}

// TestUnknownDirectiveDoesNotEatNewline: unlike every other directive, Unknown
// does not swallow the newline immediately following it — documented (render.go)
// as a deliberate choice. Now that the directive renders literally, the whole
// input round-trips byte-for-byte.
func TestUnknownDirectiveDoesNotEatNewline(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "A\n@auth\nB\n"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "A\n@auth\nB\n" {
		t.Fatalf("output = %q; want %q", got, "A\n@auth\nB\n")
	}
}

// TestUnknownDoubleAtRendersLiteral: real Blade renders `a@@b` verbatim — the
// leading '@' is literal text and `@b` is an unregistered directive left
// literal. DevDojo Framework now matches: Text "a@" + Unknown "@b" -> "a@@b".
func TestUnknownDoubleAtRendersLiteral(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "a@@b"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "a@@b" {
		t.Fatalf("output = %q; want %q", got, "a@@b")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1 (@b)", res.Diagnostics)
	}
	if d := res.Diagnostics[0]; d.Kind != KindAppTier || d.Construct != "@b" {
		t.Fatalf("diag = %+v; want app-tier/@b", d)
	}
}

// TestUnknownBetweenPairDoesNotSuppressBody: @auth/@endauth are each their own
// Unknown node (the parser never pairs them structurally), so both render
// literally and the text between them is ordinary body content — the whole
// input round-trips.
func TestUnknownBetweenPairDoesNotSuppressBody(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "@auth secret @endauth"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "@auth secret @endauth" {
		t.Fatalf("output = %q; want %q", got, "@auth secret @endauth")
	}
	if len(res.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %v; want 2 (@auth, @endauth)", res.Diagnostics)
	}
}

// --- $attributes on a page ---------------------------------------------------
//
// Since the attribute bag is modeled (2026-08-13), $attributes is bound in
// every COMPONENT scope; the only place it can be undefined is a page, where
// real Blade raises the same undefined-variable error. The old app-tier
// carve-out is gone: these pin error/undefined-variable at each call site
// that routes through diagExprError — echo, @if condition, and a bound
// component attribute evaluated in the page's scope.

func TestAttributesUndefinedOnPageEcho(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "X{{ $attributes }}Y"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "XY" {
		t.Fatalf("output = %q; want %q", got, "XY")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "undefined-variable" {
		t.Fatalf("diag = %+v; want error/undefined-variable", d)
	}
}

func TestAttributesUndefinedOnPageIfCond(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "@if ($attributes)yes @else no @endif"}
	res := Compile(fs, []string{entry}, Options{})
	// Leading space stripped by the entry-level ltrim (see TestIfConditionError);
	// trailing space preserved.
	if got := res.Files["index.html"]; got != "no " {
		t.Fatalf("output = %q; want %q", got, "no ")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "undefined-variable" {
		t.Fatalf("diag = %+v; want error/undefined-variable", d)
	}
}

func TestAttributesUndefinedOnPageBoundAttr(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                    `<x-c :label="$attributes"/>`,
		"src/components/c.blade.php": `@props(['label' => 'd'])[{{ $label ?? 'F' }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[F]" {
		t.Fatalf("output = %q; want %q", got, "[F]")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "undefined-variable" {
		t.Fatalf("diag = %+v; want error/undefined-variable", d)
	}
}

// TestAttributesDefinedNotSpecialCased: when $attributes IS bound (e.g. a page
// passes it as ordinary data), it is just a normal variable — no app-tier
// carve-out applies because there is no error to reclassify.
func TestAttributesDefinedNotSpecialCased(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "{{ $attributes }}"}
	res := Compile(fs, []string{entry}, Options{Data: map[string]any{"attributes": "real-value"}})
	if got := res.Files["index.html"]; got != "real-value" {
		t.Fatalf("output = %q; want %q", got, "real-value")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// --- parse-diag folding ------------------------------------------------------

// TestParseDiagsFoldedPage: a recoverable parser.Diag from the PAGE file (a
// stray @endif) folds into a Diagnostic{Kind: error, Construct: "@endif"} —
// the parser's specific construct, not the generic "parse" fallback — with
// File/Line/Col/Message carried over, and rendering still proceeds
// best-effort around it.
func TestParseDiagsFoldedPage(t *testing.T) {
	entry := "src/pages/index.blade.php"
	// A '>' (non-word) before @endif gives it the word boundary real Blade's \B@
	// rule requires; a space after @endif (not "endifB") keeps the trailing
	// letter from joining the name (an unmatched Unknown "endifb" vs a stray
	// @endif).
	fs := vfs.Mem{entry: ">@endif B"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "> B" {
		t.Fatalf("output = %q; want %q", got, "> B")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "@endif" {
		t.Fatalf("diag = %+v; want error/@endif", d)
	}
	if d.File != entry || d.Line != 1 || d.Message == "" {
		t.Fatalf("diag = %+v; want file %q, line 1, non-empty message", d, entry)
	}
}

// TestParseDiagsFoldedComponentDeduped: a recoverable parser.Diag from a
// COMPONENT file surfaces exactly once even when the component renders from
// two call sites in the same Compile.
func TestParseDiagsFoldedComponentDeduped(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                    `<x-c/><x-c/>`,
		"src/components/c.blade.php": `>@endif Y`,
	}, nil)
	if got := res.Files["index.html"]; got != "> Y> Y" {
		t.Fatalf("output = %q; want %q", got, "> Y> Y")
	}
	count := 0
	componentFile := "src/components/c.blade.php"
	for _, d := range res.Diagnostics {
		if d.Construct != "@endif" {
			continue
		}
		count++
		if d.Kind != KindError || d.File != componentFile || d.Line != 1 {
			t.Errorf("parse diag = %+v; want error/@endif at %s:1", d, componentFile)
		}
	}
	if count != 1 {
		t.Fatalf("parse diagnostics = %d; want 1 (all diags: %v)", count, res.Diagnostics)
	}
}

// TestFoldParseDiagsConstructs: foldParseDiags carries the parser's specific
// construct through to the render Diagnostic and falls back to "parse" only
// when a Diag has none.
func TestFoldParseDiagsConstructs(t *testing.T) {
	in := []parser.Diag{
		{File: "a", Line: 1, Col: 2, Construct: "@foreach", Message: "malformed @foreach"},
		{File: "a", Line: 3, Col: 4, Construct: "<x-hero>", Message: "unclosed <x-hero>"},
		{File: "a", Line: 5, Col: 6, Message: "constructless"},
	}
	out := foldParseDiags(in)
	if len(out) != 3 {
		t.Fatalf("folded %d diags; want 3", len(out))
	}
	for i, want := range []string{"@foreach", "<x-hero>", "parse"} {
		if out[i].Construct != want {
			t.Errorf("out[%d].Construct = %q; want %q", i, out[i].Construct, want)
		}
		if out[i].Kind != KindError || out[i].Message != in[i].Message || out[i].File != in[i].File {
			t.Errorf("out[%d] = %+v; want error kind, message/file carried over", i, out[i])
		}
	}
}

// --- entry-missing ------------------------------------------------------------

// TestEntryMissingDiagnostic: an entries[] path absent from the filesystem
// produces no output file and one error/entry-missing diagnostic pointing at
// the entry itself (Line/Col default to 1/1 — there is no source position to
// point at).
func TestEntryMissingDiagnostic(t *testing.T) {
	entry := "src/pages/index.blade.php"
	res := Compile(vfs.Mem{}, []string{entry}, Options{})
	if _, ok := res.Files["index.html"]; ok {
		t.Fatalf("expected no output for a missing entry, got %v", res.Files)
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "entry-missing" {
		t.Fatalf("diag = %+v; want error/entry-missing", d)
	}
	if d.File != entry || d.Line != 1 || d.Col != 1 {
		t.Fatalf("diag pos = %s:%d:%d; want %s:1:1", d.File, d.Line, d.Col, entry)
	}
}

// TestEntryMissingAlongsideValidEntries: a missing entry does not stop the
// rest of the entries list from compiling normally.
func TestEntryMissingAlongsideValidEntries(t *testing.T) {
	present := "src/pages/index.blade.php"
	missing := "src/pages/missing.blade.php"
	res := Compile(vfs.Mem{present: "hi"}, []string{present, missing}, Options{})
	if got := res.Files["index.html"]; got != "hi" {
		t.Fatalf("output = %q; want %q", got, "hi")
	}
	if !hasConstruct(res.Diagnostics, "entry-missing") {
		t.Fatalf("diagnostics = %v; want an entry-missing diagnostic", res.Diagnostics)
	}
}

// --- dedupe -------------------------------------------------------------------

// TestDedupeDiagnosticsTuple drives dedupeDiagnostics directly: entries
// sharing the full (File, Line, Col, Kind, Construct) tuple collapse to the
// first occurrence (its Message wins); any single differing field keeps the
// entry distinct. Order is otherwise preserved.
func TestDedupeDiagnosticsTuple(t *testing.T) {
	ds := []Diagnostic{
		{File: "a", Line: 1, Col: 2, Kind: KindError, Construct: "parse", Message: "first"},
		{File: "a", Line: 1, Col: 2, Kind: KindError, Construct: "parse", Message: "second (dropped)"},
		{File: "a", Line: 1, Col: 2, Kind: KindAppTier, Construct: "parse", Message: "different kind"},
		{File: "a", Line: 1, Col: 3, Kind: KindError, Construct: "parse", Message: "different col"},
		{File: "a", Line: 2, Col: 2, Kind: KindError, Construct: "parse", Message: "different line"},
		{File: "a", Line: 1, Col: 2, Kind: KindError, Construct: "expression", Message: "different construct"},
		{File: "b", Line: 1, Col: 2, Kind: KindError, Construct: "parse", Message: "different file"},
	}
	got := dedupeDiagnostics(ds)
	if len(got) != 6 {
		t.Fatalf("dedupeDiagnostics: got %d diags, want 6: %+v", len(got), got)
	}
	if got[0].Message != "first" {
		t.Fatalf("dedupeDiagnostics: kept message %q; want %q (first occurrence)", got[0].Message, "first")
	}
}

// TestDedupeForeachRepeatedDiagnostic: a @foreach body that raises the same
// diagnostic (identical file:line:col:kind:construct) on every iteration
// collapses to one entry — it is one bad expression in the loop body's
// source, not one problem per iterated element.
func TestDedupeForeachRepeatedDiagnostic(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "@foreach ($items as $item){{ $missing }}@endforeach"}
	res := Compile(fs, []string{entry}, Options{Data: map[string]any{"items": []any{"a", "b", "c"}}})
	count := 0
	for _, d := range res.Diagnostics {
		if d.Construct == "undefined-variable" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("undefined-variable diagnostics = %d; want 1 (deduped across iterations): %v", count, res.Diagnostics)
	}
}

// TestDedupeAcrossDuplicateEntries: the same entry listed twice in
// entries[] would otherwise raise the identical Unknown diagnostic twice
// (parsed and rendered independently, unlike components which share a
// cache); Compile's final dedupe pass collapses it to one.
func TestDedupeAcrossDuplicateEntries(t *testing.T) {
	entry := "src/pages/index.blade.php"
	fs := vfs.Mem{entry: "@auth X"}
	res := Compile(fs, []string{entry, entry}, Options{})
	count := 0
	for _, d := range res.Diagnostics {
		if d.Construct == "@auth" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("@auth diagnostics = %d; want 1 (deduped): %v", count, res.Diagnostics)
	}
}
