package parser

import (
	"testing"

	"github.com/thedevdojo/framework/blade/ast"
	"github.com/thedevdojo/framework/blade/scanner"
)

func parseSrc(t *testing.T, src string) ([]ast.Node, []Diag) {
	t.Helper()
	return Parse("t.blade.php", scanner.Scan("t.blade.php", []byte(src)))
}

func requireNoDiags(t *testing.T, diags []Diag) {
	t.Helper()
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %+v", diags)
	}
}

func TestNestedForeach(t *testing.T) {
	src := `@foreach ($rows as $row)@foreach ($row->cells as $cell){{ $cell }}@endforeach|@endforeach`
	nodes, diags := parseSrc(t, src)
	requireNoDiags(t, diags)
	outer := nodes[0].(*ast.Foreach)
	if outer.ValVar != "$row" || len(outer.Body) != 2 {
		t.Fatalf("outer = %+v", outer)
	}
	inner := outer.Body[0].(*ast.Foreach)
	if inner.ItemsExpr != "$row->cells" || inner.Body[0].(*ast.Echo).Expr != "$cell" {
		t.Fatalf("inner = %+v", inner)
	}
	if outer.Body[1].(*ast.Text).Bytes != "|" {
		t.Fatal("text after inner close must belong to outer body")
	}
}

func TestIfElseifElse(t *testing.T) {
	// Each else/elseif/endif needs a word boundary before its '@' (real Blade's
	// \B@ rule); the spaces land as trailing branch/else text.
	src := `@if ($a)A @elseif ($b)B @else C @endif`
	nodes, _ := parseSrc(t, src)
	n := nodes[0].(*ast.If)
	if len(n.Branches) != 2 || n.Branches[0].Cond != "$a" || n.Branches[1].Cond != "$b" {
		t.Fatalf("branches = %+v", n.Branches)
	}
	if n.Else[0].(*ast.Text).Bytes != " C " {
		t.Fatalf("else = %+v", n.Else)
	}
}

func TestComponentSlots(t *testing.T) {
	src := `<x-layouts.main title="Home"><x-slot:head><meta></x-slot><p>body</p></x-layouts.main>`
	nodes, _ := parseSrc(t, src)
	c := nodes[0].(*ast.Component)
	if c.Name != "layouts.main" || c.Slots["head"] == nil || len(c.Body) != 1 {
		t.Fatalf("component = %+v", c)
	}
}

func TestUnclosedIfDiag(t *testing.T) {
	_, diags := parseSrc(t, "line1\n@if ($a)\nnever closed")
	if len(diags) != 1 || diags[0].Line != 2 {
		t.Fatalf("want 1 diag at line 2, got %+v", diags)
	}
}

// --- diag-count table: strays, loop-jumps, malformed foreach args ---

func TestDiagCounts(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		diags int
	}{
		{"stray endif", "@endif", 1},
		{"stray endforeach", "@endforeach", 1},
		{"stray else", "@else x", 1},
		{"stray elseif", "@elseif ($a)x", 1},
		{"stray tag close", "</x-card>", 1},
		{"stray slot close", "</x-slot>", 1},
		{"break outside loop", "@break", 1},
		{"continue outside loop", "@continue", 1},
		{"break inside loop ok", "@foreach ($a as $b)@break @endforeach", 0},
		{"continue inside loop ok", "@foreach ($a as $b)@continue @endforeach", 0},
		{"foreach missing as", "@foreach ($rows)x @endforeach", 1},
		{"foreach empty items", "@foreach ( as $r)x @endforeach", 1},
		{"foreach bad var", "@foreach ($rows as row)x @endforeach", 1},
		{"foreach arrow missing val", "@foreach ($rows as $k => )x @endforeach", 1},
		{"foreach arrow bad key", "@foreach ($rows as k => $v)x @endforeach", 1},
		{"foreach ok key val", "@foreach ($rows as $k => $v)x @endforeach", 0},
		{"unknown directive no diag", "@auth x @endauth", 0},
		{"props no diag", "@props(['title' => ''])", 0},
		{"vite no diag", "@vite(['resources/js/app.js'])", 0},
		{"comment no diag", "{{-- hidden --}}", 0},
		{"unclosed component", "<x-card>body", 1},
		{"unclosed foreach", "@foreach ($a as $b)body", 1},
		{"nested clean", "<x-a><x-a><x-a>x</x-a></x-a></x-a>", 0},
		{"self-closing named slot", "<x-c><x-slot:x/></x-c>", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := parseSrc(t, c.src)
			if len(diags) != c.diags {
				t.Fatalf("src %q: want %d diags, got %d: %+v", c.src, c.diags, len(diags), diags)
			}
		})
	}
}

// --- three-level same-kind nesting (the regex-killer) ---

func TestThreeLevelForeach(t *testing.T) {
	// Consecutive @endforeach closes need a word boundary before each '@' (real
	// Blade's \B@ rule); the separating spaces land as trailing body text.
	src := `@foreach ($a as $x)@foreach ($b as $y)@foreach ($c as $z){{ $z }}@endforeach @endforeach @endforeach`
	nodes, diags := parseSrc(t, src)
	requireNoDiags(t, diags)
	l1 := nodes[0].(*ast.Foreach)
	l2 := l1.Body[0].(*ast.Foreach)
	l3 := l2.Body[0].(*ast.Foreach)
	if l1.ValVar != "$x" || l2.ValVar != "$y" || l3.ValVar != "$z" {
		t.Fatalf("vars = %q %q %q", l1.ValVar, l2.ValVar, l3.ValVar)
	}
	if l3.Body[0].(*ast.Echo).Expr != "$z" {
		t.Fatalf("inner echo = %+v", l3.Body[0])
	}
}

func TestThreeLevelComponent(t *testing.T) {
	src := `<x-a><x-a><x-a>deep</x-a></x-a></x-a>`
	nodes, diags := parseSrc(t, src)
	requireNoDiags(t, diags)
	a1 := nodes[0].(*ast.Component)
	a2 := a1.Body[0].(*ast.Component)
	a3 := a2.Body[0].(*ast.Component)
	if a3.Body[0].(*ast.Text).Bytes != "deep" {
		t.Fatalf("deepest body = %+v", a3.Body)
	}
}

func TestThreeLevelIf(t *testing.T) {
	// Each @endif needs a word boundary before its '@' (real Blade's \B@ rule);
	// the spaces land as trailing body text, so the innermost branch holds "X ".
	src := `@if ($a)@if ($b)@if ($c)X @endif @endif @endif`
	nodes, diags := parseSrc(t, src)
	requireNoDiags(t, diags)
	i1 := nodes[0].(*ast.If)
	i2 := i1.Branches[0].Body[0].(*ast.If)
	i3 := i2.Branches[0].Body[0].(*ast.If)
	if i3.Branches[0].Body[0].(*ast.Text).Bytes != "X " {
		t.Fatalf("deepest branch = %+v", i3.Branches[0].Body)
	}
}

// --- foreach arg shapes ---

func TestForeachKeyValue(t *testing.T) {
	nodes, diags := parseSrc(t, `@foreach ($users as $id => $user)x @endforeach`)
	requireNoDiags(t, diags)
	f := nodes[0].(*ast.Foreach)
	if f.ItemsExpr != "$users" || f.KeyVar != "$id" || f.ValVar != "$user" {
		t.Fatalf("foreach = %+v", f)
	}
}

func TestForeachAsInsideExprNotSplit(t *testing.T) {
	// "as" inside a string literal must not be treated as the separator.
	nodes, diags := parseSrc(t, `@foreach ($q->where('x as y', 1)->get() as $row)z @endforeach`)
	requireNoDiags(t, diags)
	f := nodes[0].(*ast.Foreach)
	if f.ItemsExpr != `$q->where('x as y', 1)->get()` || f.ValVar != "$row" {
		t.Fatalf("foreach = %+v", f)
	}
}

// Malformed foreach: node dropped, but the body still surfaces where the loop
// would have been.
func TestMalformedForeachKeepsBody(t *testing.T) {
	// @endforeach needs a word boundary before its '@' (real Blade's \B@ rule);
	// the space lands in the surfaced body text ("KEPT ").
	nodes, diags := parseSrc(t, `@foreach ($rows)KEPT @endforeach`)
	if len(diags) != 1 {
		t.Fatalf("want 1 diag, got %+v", diags)
	}
	if len(nodes) != 1 {
		t.Fatalf("want body surfaced as 1 node, got %+v", nodes)
	}
	if _, isForeach := nodes[0].(*ast.Foreach); isForeach {
		t.Fatalf("loop node should be dropped, got %+v", nodes[0])
	}
	if nodes[0].(*ast.Text).Bytes != "KEPT " {
		t.Fatalf("body text = %+v", nodes[0])
	}
}

// --- slots ---

func TestSlotInIfIsStray(t *testing.T) {
	// A slot opened inside @if inside a component is NOT directly inside the
	// component body: Diag, and its contents parse as normal body nodes.
	src := `<x-c>@if ($a)<x-slot:head>H</x-slot>@endif</x-c>`
	nodes, diags := parseSrc(t, src)
	if len(diags) == 0 {
		t.Fatal("want at least one diag for the misplaced slot")
	}
	c := nodes[0].(*ast.Component)
	if c.Slots["head"] != nil {
		t.Fatalf("slot must NOT be captured on the component: %+v", c.Slots)
	}
	branch := c.Body[0].(*ast.If).Branches[0]
	if branch.Body[0].(*ast.Text).Bytes != "H" {
		t.Fatalf("slot contents must surface as normal body text: %+v", branch.Body)
	}
}

// TestSelfClosingNamedSlot: a self-closing named slot <x-slot:x/> is captured on
// the component with an empty (nil) body and produces no diagnostic.
func TestSelfClosingNamedSlot(t *testing.T) {
	nodes, diags := parseSrc(t, `<x-c><x-slot:x/></x-c>`)
	requireNoDiags(t, diags)
	c := nodes[0].(*ast.Component)
	body, ok := c.Slots["x"]
	if !ok {
		t.Fatalf("named slot \"x\" not captured on the component: %+v", c.Slots)
	}
	if len(body) != 0 {
		t.Fatalf("self-closing slot must have an empty body, got %+v", body)
	}
}

func TestDuplicateSlotLastWins(t *testing.T) {
	src := `<x-c><x-slot:a>1</x-slot><x-slot:a>2</x-slot></x-c>`
	nodes, diags := parseSrc(t, src)
	if len(diags) != 1 {
		t.Fatalf("want 1 duplicate-slot diag, got %+v", diags)
	}
	c := nodes[0].(*ast.Component)
	if c.Slots["a"][0].(*ast.Text).Bytes != "2" {
		t.Fatalf("last slot must win: %+v", c.Slots["a"])
	}
}

// TestNamedSlotDashCamelization: a named slot whose source name contains a dash
// is captured under its camelized BINDING name (real Blade applies Str::camel
// to inline slot names containing '-'); an underscore-only name stays raw; a
// mixed name camelizes fully (Str::camel splits on '-' AND '_'). Oracle-probed
// against Laravel 12.64.
func TestNamedSlotDashCamelization(t *testing.T) {
	src := `<x-c><x-slot:foo-bar>A</x-slot><x-slot:foo_bar>B</x-slot><x-slot:foo-bar_baz>C</x-slot></x-c>`
	nodes, diags := parseSrc(t, src)
	requireNoDiags(t, diags)
	c := nodes[0].(*ast.Component)
	for key, want := range map[string]string{"fooBar": "A", "foo_bar": "B", "fooBarBaz": "C"} {
		body, ok := c.Slots[key]
		if !ok {
			t.Fatalf("slot %q not captured (have %+v)", key, c.Slots)
		}
		if body[0].(*ast.Text).Bytes != want {
			t.Errorf("slot %q body = %+v; want %q", key, body[0], want)
		}
	}
	if _, raw := c.Slots["foo-bar"]; raw {
		t.Fatal("dashed slot must not also bind under its raw name")
	}
}

// TestDuplicateSlotAcrossSpellings: <x-slot:foo-bar> and <x-slot:fooBar> bind
// the SAME variable in real Blade (the dashed spelling camelizes), so the
// parser treats them as duplicates — diagnosed, document-order last one wins.
func TestDuplicateSlotAcrossSpellings(t *testing.T) {
	src := `<x-c><x-slot:foo-bar>1</x-slot><x-slot:fooBar>2</x-slot></x-c>`
	nodes, diags := parseSrc(t, src)
	if len(diags) != 1 {
		t.Fatalf("want 1 duplicate-slot diag, got %+v", diags)
	}
	c := nodes[0].(*ast.Component)
	if c.Slots["fooBar"][0].(*ast.Text).Bytes != "2" {
		t.Fatalf("last slot must win under the binding name: %+v", c.Slots)
	}
}

// --- misc node shapes ---

func TestUnknownDirective(t *testing.T) {
	nodes, diags := parseSrc(t, `@auth secret @endauth`)
	requireNoDiags(t, diags)
	if u := nodes[0].(*ast.Unknown); u.Name != "auth" || u.Src != "@auth" {
		t.Fatalf("want @auth Unknown with Src %q, got %+v", "@auth", u)
	}
	// @endauth is also unknown (not a structural closer) — no diag.
	if u := nodes[len(nodes)-1].(*ast.Unknown); u.Name != "endauth" || u.Src != "@endauth" {
		t.Fatalf("want @endauth Unknown with Src %q, got %+v", "@endauth", u)
	}
}

// TestUnknownDirectiveSrcWithArgs: the Unknown node carries the directive's full
// original source slice (name + args, verbatim spacing/casing) so the renderer
// can emit it literally.
func TestUnknownDirectiveSrcWithArgs(t *testing.T) {
	nodes, diags := parseSrc(t, `A @Media (min-width: 600px) B`)
	requireNoDiags(t, diags)
	u := nodes[1].(*ast.Unknown)
	if u.Name != "media" {
		t.Fatalf("Name = %q; want %q", u.Name, "media")
	}
	if u.Src != "@Media (min-width: 600px)" {
		t.Fatalf("Src = %q; want %q", u.Src, "@Media (min-width: 600px)")
	}
}

func TestCommentDropped(t *testing.T) {
	nodes, diags := parseSrc(t, `A{{-- c --}}B`)
	requireNoDiags(t, diags)
	if len(nodes) != 2 || nodes[0].(*ast.Text).Bytes != "A" || nodes[1].(*ast.Text).Bytes != "B" {
		t.Fatalf("comment should vanish, got %+v", nodes)
	}
}

func TestRawEcho(t *testing.T) {
	nodes, _ := parseSrc(t, `{!! $html !!}`)
	e := nodes[0].(*ast.Echo)
	if !e.Raw || e.Expr != "$html" {
		t.Fatalf("raw echo = %+v", e)
	}
}

func TestConditionalLoopJump(t *testing.T) {
	nodes, diags := parseSrc(t, `@foreach ($a as $b)@continue($b->skip)@break($b->stop)@endforeach`)
	requireNoDiags(t, diags)
	f := nodes[0].(*ast.Foreach)
	cont := f.Body[0].(*ast.LoopJump)
	brk := f.Body[1].(*ast.LoopJump)
	if !cont.Continue || cont.Cond != "$b->skip" {
		t.Fatalf("continue = %+v", cont)
	}
	if brk.Continue || brk.Cond != "$b->stop" {
		t.Fatalf("break = %+v", brk)
	}
}

func TestPropsAndVite(t *testing.T) {
	nodes, diags := parseSrc(t, `@props(['title' => 'Home'])@vite(['resources/js/app.js'])`)
	requireNoDiags(t, diags)
	if nodes[0].(*ast.Props).Raw != "['title' => 'Home']" {
		t.Fatalf("props = %+v", nodes[0])
	}
	if nodes[1].(*ast.Vite).Raw != "['resources/js/app.js']" {
		t.Fatalf("vite = %+v", nodes[1])
	}
}

func TestUnclosedComponentDiagPos(t *testing.T) {
	_, diags := parseSrc(t, "\n  <x-card>body")
	if len(diags) != 1 {
		t.Fatalf("want 1 diag, got %+v", diags)
	}
	// The opening <x-card> sits at line 2, col 3.
	if diags[0].Line != 2 || diags[0].Col != 3 {
		t.Fatalf("diag should point at the opening tag (2:3), got %+v", diags[0])
	}
}

func TestSelfClosingComponent(t *testing.T) {
	nodes, diags := parseSrc(t, `<x-icon name="star"/>after`)
	requireNoDiags(t, diags)
	c := nodes[0].(*ast.Component)
	if !c.SelfClose || c.Name != "icon" || len(c.Body) != 0 {
		t.Fatalf("self-closing component = %+v", c)
	}
	if nodes[1].(*ast.Text).Bytes != "after" {
		t.Fatalf("text after self-close = %+v", nodes[1])
	}
}
