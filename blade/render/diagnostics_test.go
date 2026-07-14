package render

import (
	"testing"

	"github.com/thedevdojo/pocketknife/vfs"
)

// TestUnknownDirectiveAppTier: an ast.Unknown directive (any directive outside
// the closed subset — @auth, @php, @include, @extends, @section, @csrf, …)
// renders nothing and raises exactly one app-tier diagnostic whose Construct
// is "@"+Name, at the directive's own position.
func TestUnknownDirectiveAppTier(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	// A space (not "XauthY") after the directive name: the scanner's directive
	// reader is alnum-greedy, so a letter glued directly onto the name would
	// become part of it ("authy") rather than following text.
	fs := vfs.Mem{entry: "X@auth Y"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "X Y" {
		t.Fatalf("output = %q; want %q", got, "X Y")
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

// TestUnknownDirectiveDoesNotEatNewline: unlike every other directive,
// Unknown does not swallow the newline immediately following it — documented
// (render.go) as a deliberate choice, since it renders nothing and so has no
// compiled close-tag whose swallow rule would apply.
func TestUnknownDirectiveDoesNotEatNewline(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	fs := vfs.Mem{entry: "A\n@auth\nB\n"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "A\n\nB\n" {
		t.Fatalf("output = %q; want %q", got, "A\n\nB\n")
	}
}

// TestUnknownBetweenPairDoesNotSuppressBody: @auth/@endauth are each their own
// Unknown node (the parser never pairs them structurally), so the text between
// them is ordinary body content and renders unconditionally.
func TestUnknownBetweenPairDoesNotSuppressBody(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	fs := vfs.Mem{entry: "@auth secret @endauth"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != " secret " {
		t.Fatalf("output = %q; want %q", got, " secret ")
	}
	if len(res.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %v; want 2 (@auth, @endauth)", res.Diagnostics)
	}
}

// --- $attributes carve-out --------------------------------------------------
//
// $attributes is syntactically a plain, valid variable — it evaluates as
// undefined like any other unbound var — but names a Blade feature
// (the dynamic attribute bag) this engine does not model. An undefined
// `$attributes` is reclassified app-tier/$attributes instead of
// error/undefined-variable, at every call site that routes through
// diagExprError: echo, @if condition, and a bound component attribute.

func TestAttributesEchoAppTier(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	fs := vfs.Mem{entry: "X{{ $attributes }}Y"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "XY" {
		t.Fatalf("output = %q; want %q", got, "XY")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "$attributes" {
		t.Fatalf("diag = %+v; want app-tier/$attributes", d)
	}
}

func TestAttributesIfCondAppTier(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	fs := vfs.Mem{entry: "@if ($attributes)yes@else no@endif"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != " no" {
		t.Fatalf("output = %q; want %q", got, " no")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "$attributes" {
		t.Fatalf("diag = %+v; want app-tier/$attributes", d)
	}
}

func TestAttributesBoundAttrAppTier(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `<x-c :label="$attributes"/>`,
		"resources/views/components/c.blade.html": `@props(['label' => 'd'])[{{ $label ?? 'F' }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[F]" {
		t.Fatalf("output = %q; want %q", got, "[F]")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "$attributes" {
		t.Fatalf("diag = %+v; want app-tier/$attributes", d)
	}
}

// TestAttributesDefinedNotSpecialCased: when $attributes IS bound (e.g. a page
// passes it as ordinary data), it is just a normal variable — no app-tier
// carve-out applies because there is no error to reclassify.
func TestAttributesDefinedNotSpecialCased(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
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
// stray @endif) folds into a Diagnostic{Kind: error, Construct: "parse"} with
// File/Line/Col/Message carried over, and rendering still proceeds
// best-effort around it.
func TestParseDiagsFoldedPage(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
	// Space after @endif, not "endifB": glued directly on, the trailing letter
	// would join the directive name (making it an unmatched Unknown "endifb"
	// rather than a recognized stray @endif).
	fs := vfs.Mem{entry: "A@endif B"}
	res := Compile(fs, []string{entry}, Options{})
	if got := res.Files["index.html"]; got != "A B" {
		t.Fatalf("output = %q; want %q", got, "A B")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly 1", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "parse" {
		t.Fatalf("diag = %+v; want error/parse", d)
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
		pageEntry: `<x-c/><x-c/>`,
		"resources/views/components/c.blade.html": `X@endif Y`,
	}, nil)
	if got := res.Files["index.html"]; got != "X YX Y" {
		t.Fatalf("output = %q; want %q", got, "X YX Y")
	}
	count := 0
	componentFile := "resources/views/components/c.blade.html"
	for _, d := range res.Diagnostics {
		if d.Construct != "parse" {
			continue
		}
		count++
		if d.Kind != KindError || d.File != componentFile || d.Line != 1 {
			t.Errorf("parse diag = %+v; want error/parse at %s:1", d, componentFile)
		}
	}
	if count != 1 {
		t.Fatalf("parse diagnostics = %d; want 1 (all diags: %v)", count, res.Diagnostics)
	}
}

// --- entry-missing ------------------------------------------------------------

// TestEntryMissingDiagnostic: an entries[] path absent from the filesystem
// produces no output file and one error/entry-missing diagnostic pointing at
// the entry itself (Line/Col default to 1/1 — there is no source position to
// point at).
func TestEntryMissingDiagnostic(t *testing.T) {
	entry := "resources/views/pages/index.blade.html"
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
	present := "resources/views/pages/index.blade.html"
	missing := "resources/views/pages/missing.blade.html"
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
	entry := "resources/views/pages/index.blade.html"
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
	entry := "resources/views/pages/index.blade.html"
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
