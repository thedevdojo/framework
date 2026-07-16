package render

import (
	"testing"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/scanner"
	"github.com/thedevdojo/pocketknife/vfs"
)

// data is the render-test shorthand for Options.Data (the protocol's decoded
// JSON map). It is assignable to map[string]any, so it drops straight into
// Options{Data: …}.
type data map[string]any

// renderSrc compiles a single-view source against a Mem fs and returns the
// rendered bytes plus any diagnostics.
func renderSrc(t *testing.T, src string, d data) (string, []Diagnostic) {
	t.Helper()
	entry := "resources/views/pages/index.blade.html"
	res := Compile(vfs.Mem{entry: src}, []string{entry}, Options{Data: d})
	return res.Files["index.html"], res.Diagnostics
}

// TestControlFlow covers the brief's six rows verbatim plus map iteration
// (sorted keys), nested-loop shadowing ($loop innermost), and the $loop meta
// fields (index/count/even/odd). None of these rows produce a diagnostic.
func TestControlFlow(t *testing.T) {
	cases := []struct {
		src  string
		data data
		want string
	}{
		// --- brief rows (verbatim) ---
		// Each else/elseif/endif needs a word boundary before its '@' (real
		// Blade's \B@ rule); the space lands in the taken branch's output.
		{`@if ($n == 1)one @elseif ($n == 2)two @else many @endif`, data{"n": 2}, "two "},
		{`@foreach ($xs as $x){{ $loop->iteration }}:{{ $x }};@endforeach`, data{"xs": []any{"a", "b"}}, "1:a;2:b;"},
		{`@foreach ($xs as $k => $x){{ $k }}={{ $x }};@endforeach`, data{"xs": []any{"a"}}, "0=a;"},
		// The final @endforeach needs a word boundary before it; the newline
		// after @endif is eaten, so the loop body still renders clean [123]
		// (verified byte-identical against real Blade).
		{"@foreach ($xs as $x)@if ($loop->first)[@endif{{ $x }}@if ($loop->last)]@endif\n@endforeach",
			data{"xs": []any{1, 2, 3}}, "[123]"},
		{`@foreach ($xs as $x){{ $x }}@break($loop->iteration == 2)@endforeach`, data{"xs": []any{1, 2, 3}}, "12"},
		{`@foreach ($xs as $x)@continue($x == 2){{ $x }}@endforeach`, data{"xs": []any{1, 2, 3}}, "13"},

		// --- protocol-data object iteration: a data object arrives already
		// decoded by encoding/json (key order lost), so it becomes a sorted-key
		// *Object and iterates in SORTED order. (File-sourced $site keeps JSON
		// insertion order — see fixtures/object-iteration-order.) ---
		{`@foreach ($m as $k => $v){{ $k }}={{ $v }};@endforeach`,
			data{"m": map[string]any{"b": 2, "a": 1, "c": 3}}, "a=1;b=2;c=3;"},

		// --- nested loops: $loop is the INNERMOST loop; outer vars stay visible;
		// the inner loop restarts its iteration count each pass. ---
		{`@foreach ($xs as $x)@foreach ($ys as $y){{ $x }}{{ $y }}#{{ $loop->iteration }};@endforeach|@endforeach`,
			data{"xs": []any{"a", "b"}, "ys": []any{1, 2}}, "a1#1;a2#2;|b1#1;b2#2;|"},

		// --- $loop meta: index (0-based), count, even/odd (by ITERATION). ---
		{`@foreach ($xs as $x)i{{ $loop->index }}n{{ $loop->count }}e{{ $loop->even }}o{{ $loop->odd }};@endforeach`,
			data{"xs": []any{"a", "b", "c"}}, "i0n3eo1;i1n3e1o;i2n3eo1;"},
	}
	for _, c := range cases {
		got, diags := renderSrc(t, c.src, c.data)
		if got != c.want {
			t.Errorf("render(%q) = %q; want %q", c.src, got, c.want)
		}
		if len(diags) != 0 {
			t.Errorf("render(%q): unexpected diagnostics: %v", c.src, diags)
		}
	}
}

// TestLoopIsObject: $loop is a stdClass. Its seven v1 meta properties read
// cleanly, but a bare access to remaining/depth/parent is APP-TIER (spec §3.2 —
// the object model finally fulfills that promise; a map used to yield silent
// nil), and any other absent member is an undefined-property error.
func TestLoopIsObject(t *testing.T) {
	// remaining is app-tier.
	got, diags := renderSrc(t, "@foreach ($xs as $x){{ $loop->remaining }}@endforeach", data{"xs": []any{1, 2}})
	if got != "" {
		t.Errorf("output = %q; want empty (app-tier render)", got)
	}
	if len(diags) != 1 || diags[0].Kind != KindAppTier {
		t.Fatalf("diagnostics = %v; want one app-tier for $loop->remaining", diags)
	}

	// A truly unknown member is an undefined-property error.
	_, diags = renderSrc(t, "@foreach ($xs as $x){{ $loop->typo }}@endforeach", data{"xs": []any{1}})
	if len(diags) != 1 || diags[0].Kind != KindError || diags[0].Construct != "undefined-property" {
		t.Fatalf("diagnostics = %v; want one error/undefined-property for $loop->typo", diags)
	}
}

// TestForeachNonIterable: a @foreach over a scalar/nil value diagnoses
// error/foreach at the node position and renders the block empty (surrounding
// text preserved).
func TestForeachNonIterable(t *testing.T) {
	for _, d := range []data{{"xs": 7}, {"xs": "nope"}, {"xs": nil}, {"xs": true}} {
		// A '>' (non-word) before @foreach gives it a word boundary, and a
		// separator after @endforeach avoids merging into the directive name
		// (@endforeachB would scan as one word — both matching Blade).
		got, diags := renderSrc(t, ">@foreach ($xs as $x){{ $x }}@endforeach B", d)
		if got != "> B" {
			t.Errorf("data %v: output = %q; want %q", d, got, "> B")
		}
		if len(diags) != 1 || diags[0].Kind != KindError || diags[0].Construct != "foreach" {
			t.Errorf("data %v: diagnostics = %v; want one error/foreach", d, diags)
		}
	}
}

// TestBreakCondError: a break whose condition fails to evaluate is diagnosed
// (per Task 6's mapping) and treated FALSE — the loop keeps running to the end.
func TestBreakCondError(t *testing.T) {
	got, diags := renderSrc(t, "@foreach ($xs as $x){{ $x }}@break($missing)@endforeach", data{"xs": []any{1, 2, 3}})
	if got != "123" {
		t.Fatalf("output = %q; want %q", got, "123")
	}
	if len(diags) == 0 {
		t.Fatal("want at least one diagnostic for the break condition error")
	}
	for _, d := range diags {
		if d.Construct != "undefined-variable" {
			t.Errorf("diag construct = %q; want undefined-variable", d.Construct)
		}
	}
}

// TestContinueCondError: a continue whose condition fails is likewise treated
// FALSE — every item renders.
func TestContinueCondError(t *testing.T) {
	got, diags := renderSrc(t, "@foreach ($xs as $x)@continue($missing){{ $x }}@endforeach", data{"xs": []any{1, 2, 3}})
	if got != "123" {
		t.Fatalf("output = %q; want %q", got, "123")
	}
	if len(diags) == 0 {
		t.Fatal("want at least one diagnostic for the continue condition error")
	}
}

// TestLoopJumpOutsideLoop: a LoopJump reached with no active loop (a parser
// edge case — a dropped malformed @foreach, or a jump inside a component body)
// diagnoses error/loop-jump, renders nothing, and never panics.
func TestLoopJumpOutsideLoop(t *testing.T) {
	for _, cont := range []bool{false, true} {
		r := &renderer{scope: dataScope{}}
		p := scanner.Pos{File: "x.blade.html", Line: 3, Col: 5}
		eat := r.renderNode(&ast.LoopJump{Continue: cont, P: p}, false)
		if r.sb.String() != "" {
			t.Errorf("continue=%v: output = %q; want empty", cont, r.sb.String())
		}
		if len(r.diags) != 1 || r.diags[0].Kind != KindError || r.diags[0].Construct != "loop-jump" {
			t.Errorf("continue=%v: diagnostics = %v; want one error/loop-jump", cont, r.diags)
		}
		if !eat {
			t.Errorf("continue=%v: a loop-jump directive should still eat the following newline", cont)
		}
	}
}

// TestBreakBreaksInnermostOnly: an unconditional @break inside a nested loop
// stops only the inner loop; the outer loop keeps iterating.
func TestBreakBreaksInnermostOnly(t *testing.T) {
	// @break needs a word boundary before the inner @endforeach; the space is
	// unreachable body content (the break exits first), so output is unchanged.
	src := `@foreach ($xs as $x)[{{ $x }}@foreach ($ys as $y){{ $y }}@break @endforeach]@endforeach`
	got, diags := renderSrc(t, src, data{"xs": []any{"a", "b"}, "ys": []any{1, 2, 3}})
	if got != "[a1][b1]" {
		t.Fatalf("output = %q; want %q", got, "[a1][b1]")
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}
