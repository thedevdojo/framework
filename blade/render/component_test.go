package render

import (
	"testing"

	"github.com/thedevdojo/framework/blade/expr"
	"github.com/thedevdojo/framework/vfs"
)

// TestParseProps drives the dedicated literal-array parser: string/number/bool/
// null values, bare (default-less) entries, the empty array, empty/flat-list
// array defaults, the valid-but-unsupported array shapes (nested/associative —
// declared without a default, one precise issue each), and the malformed rows
// that must report ok=false so the renderer can treat props as empty.
func TestParseProps(t *testing.T) {
	cases := []struct {
		raw    string
		ok     bool
		issues int
		want   map[string]propDecl
	}{
		{`['heading' => 'Default']`, true, 0, map[string]propDecl{"heading": {hasDefault: true, def: "Default"}}},
		{`['heading' => 'Default', 'bare']`, true, 0, map[string]propDecl{
			"heading": {hasDefault: true, def: "Default"},
			"bare":    {hasDefault: false},
		}},
		{`["dq" => "v"]`, true, 0, map[string]propDecl{"dq": {hasDefault: true, def: "v"}}},
		{`['n' => 42, 'f' => 2.5, 'b' => true, 'z' => false, 'nul' => null]`, true, 0, map[string]propDecl{
			"n":   {hasDefault: true, def: int64(42)},
			"f":   {hasDefault: true, def: 2.5},
			"b":   {hasDefault: true, def: true},
			"z":   {hasDefault: true, def: false},
			"nul": {hasDefault: true, def: nil},
		}},
		{`[]`, true, 0, map[string]propDecl{}},
		{`['a' => 'x', 'b' => 'y',]`, true, 0, map[string]propDecl{ // trailing comma tolerated
			"a": {hasDefault: true, def: "x"},
			"b": {hasDefault: true, def: "y"},
		}},
		{`["bare"]`, true, 0, map[string]propDecl{"bare": {hasDefault: false}}},

		// array defaults: empty and flat scalar lists are modeled
		{`['menu' => []]`, true, 0, map[string]propDecl{"menu": {hasDefault: true, def: []expr.Value{}}}},
		{`['items' => ['a', 'b']]`, true, 0, map[string]propDecl{"items": {hasDefault: true, def: []expr.Value{"a", "b"}}}},
		{`['mix' => [1, 2.5, true, null, 'x',]]`, true, 0, map[string]propDecl{ // trailing comma tolerated here too
			"mix": {hasDefault: true, def: []expr.Value{int64(1), 2.5, true, nil, "x"}},
		}},

		// valid-but-unsupported array defaults: declared without a default + issue
		{`['cfg' => ['a' => 1]]`, true, 1, map[string]propDecl{"cfg": {hasDefault: false}}},
		{`['grid' => [[1, 2], [3]]]`, true, 1, map[string]propDecl{"grid": {hasDefault: false}}},
		{`['cfg' => ['a' => 1], 'ok' => 'x']`, true, 1, map[string]propDecl{
			"cfg": {hasDefault: false},
			"ok":  {hasDefault: true, def: "x"},
		}},

		// malformed rows → ok=false
		{`'heading' => 'Default'`, false, 0, nil},  // no surrounding brackets
		{`['a' => ]`, false, 0, nil},               // missing value
		{`['a' => @foo]`, false, 0, nil},           // non-literal value
		{`[42]`, false, 0, nil},                    // bare non-string entry
		{`['a' => 'x' 'b' => 'y']`, false, 0, nil}, // missing comma
		{`['a' => [@foo]]`, false, 0, nil},         // non-literal list element
		{`['a' => ['x']`, false, 0, nil},           // unbalanced array default
		{``, false, 0, nil},                        // empty raw (e.g. @props())
	}
	for _, c := range cases {
		got, issues, ok := parseProps(c.raw)
		if ok != c.ok {
			t.Errorf("parseProps(%q) ok = %v; want %v", c.raw, ok, c.ok)
			continue
		}
		if len(issues) != c.issues {
			t.Errorf("parseProps(%q) issues = %v; want %d", c.raw, issues, c.issues)
		}
		if !c.ok {
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("parseProps(%q) = %v; want %v", c.raw, got, c.want)
			continue
		}
		for name, wd := range c.want {
			gd, ok := got[name]
			if !ok || gd.hasDefault != wd.hasDefault || !valueEq(gd.def, wd.def) {
				t.Errorf("parseProps(%q)[%q] = %+v; want %+v", c.raw, name, gd, wd)
			}
		}
	}
}

func valueEq(a, b expr.Value) bool {
	as, aok := a.([]expr.Value)
	bs, bok := b.([]expr.Value)
	if aok || bok {
		if !aok || !bok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !valueEq(as[i], bs[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// TestCamelCase covers the kebab/snake → camelCase mapping real Blade applies
// when matching attribute names to declared prop names.
func TestCamelCase(t *testing.T) {
	cases := map[string]string{
		"foo-bar":  "fooBar",
		"title":    "title",
		"data-x-y": "dataXY",
		"fooBar":   "fooBar",
		"foo_bar":  "fooBar",
		"a":        "a",
		"":         "",
		"has-icon": "hasIcon",
	}
	for in, want := range cases {
		if got := camelCase(in); got != want {
			t.Errorf("camelCase(%q) = %q; want %q", in, got, want)
		}
	}
}

// compileMem is a test shorthand: compile pages/components from an in-memory fs.
func compileMem(t *testing.T, files map[string]string, d map[string]any) Result {
	t.Helper()
	fs := vfs.Mem{}
	for k, v := range files {
		fs[k] = v
	}
	return Compile(fs, []string{"resources/views/pages/index.blade.php"}, Options{Data: d})
}

const pageEntry = "resources/views/pages/index.blade.php"

// TestComponentScopeIsolation: page-level data does NOT leak into a component.
func TestComponentScopeIsolation(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `A<x-probe/>B`,
		"resources/views/components/probe.blade.php": `[{{ $x ?? 'unset' }}]`,
	}, map[string]any{"x": 1})
	if got := res.Files["index.html"]; got != "A[unset]B" {
		t.Fatalf("output = %q; want %q", got, "A[unset]B")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestComponentUndeclaredAttrDropped: an attribute not declared in @props is
// silently dropped (no diagnostic) — the dialect forbids $attributes.
func TestComponentUndeclaredAttrDropped(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c extra="v" data-thing="q"/>`,
		"resources/views/components/c.blade.php": `[{{ $slot }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[]" {
		t.Fatalf("output = %q; want %q", got, "[]")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestComponentDefaultsAndOverride: default used when unset; static attr and
// kebab→camel attr override.
func TestComponentDefaultsAndOverride(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c/>|<x-c heading="Given" foo-bar="hit"/>`,
		"resources/views/components/c.blade.php": `@props(['heading' => 'Default', 'fooBar' => 'miss'])[{{ $heading }}/{{ $fooBar }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[Default/miss]|[Given/hit]" {
		t.Fatalf("output = %q; want %q", got, "[Default/miss]|[Given/hit]")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestComponentBoundAttrCallerScope: a :bound attr evaluates in the caller's
// scope at the tag site; a static attr passes its string verbatim.
func TestComponentBoundAttrCallerScope(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c :label="$who" note="lit"/>`,
		"resources/views/components/c.blade.php": `@props(['label' => 'd', 'note' => 'd2'])[{{ $label }}/{{ $note }}]`,
	}, map[string]any{"who": "Ada"})
	if got := res.Files["index.html"]; got != "[Ada/lit]" {
		t.Fatalf("output = %q; want %q", got, "[Ada/lit]")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestComponentBoundAttrEvalError: a bound attr whose expression fails to
// evaluate is diagnosed (Task 6 mapping) and the prop becomes nil. That nil is
// an ERROR SENTINEL: unlike a genuine passed null (which falls through to the
// prop's default — see TestPassedNullFallsBackToDefault), it leaves the prop
// nil, so `?? 'F'` shows the guard rather than the default masking the break.
func TestComponentBoundAttrEvalError(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c :label="$missing"/>`,
		"resources/views/components/c.blade.php": `@props(['label' => 'd'])[{{ $label ?? 'F' }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[F]" {
		t.Fatalf("output = %q; want %q", got, "[F]")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Construct != "undefined-variable" {
		t.Fatalf("diagnostics = %v; want one undefined-variable", res.Diagnostics)
	}
}

// TestComponentStaticAttrInterpolation: a STATIC attribute whose value carries a
// {{ }} interpolation is real Blade this engine does not model — the value passes
// through literally and an app-tier attribute-interpolation diagnostic fires at
// the ATTRIBUTE's position.
func TestComponentStaticAttrInterpolation(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `<x-hero heading="Hi {{ $name }}"/>`,
		"resources/views/components/hero.blade.php": `@props(['heading'])[{{ $heading }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[Hi {{ $name }}]" {
		t.Fatalf("output = %q; want %q (braces pass through literally)", got, "[Hi {{ $name }}]")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindAppTier || d.Construct != "attribute-interpolation" {
		t.Fatalf("diag = %+v; want app-tier/attribute-interpolation", d)
	}
	// `heading` begins at column 9 (`<x-hero ` is 8 bytes).
	if d.File != pageEntry || d.Line != 1 || d.Col != 9 {
		t.Fatalf("diag pos = %s:%d:%d; want %s:1:9", d.File, d.Line, d.Col, pageEntry)
	}
}

// TestComponentRawAttrInterpolationNoFalsePositive: a static value with no
// interpolation braces passes through with no diagnostic.
func TestComponentStaticAttrNoInterpolation(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `<x-hero heading="Plain"/>`,
		"resources/views/components/hero.blade.php": `@props(['heading'])[{{ $heading }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[Plain]" {
		t.Fatalf("output = %q; want %q", got, "[Plain]")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestBoundAttrDiagnosticCarriesAttrPos pins that a bound-attribute eval error is
// reported at the ATTRIBUTE's position (scanner.Attr.Pos), not the tag's — so
// Attr.Pos is load-bearing, not decorative.
func TestBoundAttrDiagnosticCarriesAttrPos(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c :label="$missing"/>`,
		"resources/views/components/c.blade.php": `@props(['label' => 'd'])[{{ $label ?? 'F' }}]`,
	}, nil)
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want exactly one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Construct != "undefined-variable" {
		t.Fatalf("diag = %+v; want undefined-variable", d)
	}
	// `:label` begins at column 6 (`<x-c ` is 5 bytes); the diagnostic must ride
	// the attribute position, not the tag's column 1.
	if d.File != pageEntry || d.Line != 1 || d.Col != 6 {
		t.Fatalf("diag pos = %s:%d:%d; want %s:1:6", d.File, d.Line, d.Col, pageEntry)
	}
}

// TestComponentMissingMarker: a missing component emits the byte-exact marker
// and a component-missing diagnostic at the tag position.
func TestComponentMissingMarker(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `X<x-sections.hero/>Y`,
	}, nil)
	want := `X<!-- devdojo:missing component "sections.hero" -->Y`
	if got := res.Files["index.html"]; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v; want one", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Kind != KindError || d.Construct != "component-missing" {
		t.Fatalf("diag = %+v; want error/component-missing", d)
	}
	if d.File != pageEntry || d.Line != 1 || d.Col != 2 {
		t.Fatalf("diag pos = %s:%d:%d; want %s:1:2", d.File, d.Line, d.Col, pageEntry)
	}
	// The path the resolver LOOKED FOR joins deps even though it is absent, so
	// a render-on-save host re-renders this output when the user later creates
	// resources/views/components/sections/hero.blade.php.
	deps := res.Deps["index.html"]
	wantDeps := []string{pageEntry, "resources/views/components/sections/hero.blade.php"}
	if len(deps) != len(wantDeps) {
		t.Fatalf("deps = %v; want %v", deps, wantDeps)
	}
	for i := range wantDeps {
		if deps[i] != wantDeps[i] {
			t.Fatalf("deps = %v; want %v", deps, wantDeps)
		}
	}
}

// TestComponentRecursionCap: a self-including component renders a bounded number
// of levels and raises exactly one component-recursion diagnostic.
func TestComponentRecursionCap(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `<x-recur/>`,
		"resources/views/components/recur.blade.php": `R<x-recur/>`,
	}, nil)
	got := res.Files["index.html"]
	if len(got) != componentMaxDepth {
		t.Fatalf("output = %q (len %d); want %d R's", got, len(got), componentMaxDepth)
	}
	recur := 0
	for _, d := range res.Diagnostics {
		if d.Construct == "component-recursion" {
			recur++
			if d.Kind != KindError {
				t.Errorf("recursion diag kind = %q; want error", d.Kind)
			}
		}
	}
	if recur != 1 {
		t.Fatalf("component-recursion diagnostics = %d; want 1 (all diags: %v)", recur, res.Diagnostics)
	}
}

// TestComponentDeps: a component that itself renders another component adds both
// files to the output's deps, entry first, deduped.
func TestComponentDeps(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-a/><x-a/>`,
		"resources/views/components/a.blade.php": `<x-b/>`,
		"resources/views/components/b.blade.php": `b`,
	}, nil)
	deps := res.Deps["index.html"]
	want := []string{
		pageEntry,
		"resources/views/components/a.blade.php",
		"resources/views/components/b.blade.php",
	}
	if len(deps) != len(want) {
		t.Fatalf("deps = %v; want %v", deps, want)
	}
	for i := range want {
		if deps[i] != want[i] {
			t.Fatalf("deps = %v; want %v", deps, want)
		}
	}
}

// TestSlotTrimmedAndRaw: default and named slots are PHP-trimmed and emitted as
// raw HTML (not escaped) by {{ }}.
func TestSlotTrimmedAndRaw(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: "<x-wrap>\n   <b>hi</b>\n<x-slot:title>\n  T&T\n</x-slot>\n</x-wrap>",
		"resources/views/components/wrap.blade.php": `D[{{ $slot }}]T[{{ $title }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "D[<b>hi</b>]T[T&T]" {
		t.Fatalf("output = %q; want %q", got, "D[<b>hi</b>]T[T&T]")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
}

// TestSelfCloseEmptySlot: a self-closing tag yields an empty slot.
func TestSelfCloseEmptySlot(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c/>`,
		"resources/views/components/c.blade.php": `[{{ $slot }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[]" {
		t.Fatalf("output = %q; want %q", got, "[]")
	}
}

// TestSlotRefsLoopVar: slot content is rendered in the CALLER's scope, so it can
// reference an enclosing loop variable.
func TestSlotRefsLoopVar(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `@foreach ($xs as $x)<x-c>{{ $x }}</x-c>@endforeach`,
		"resources/views/components/c.blade.php": `[{{ $slot }}]`,
	}, map[string]any{"xs": []any{"a", "b"}})
	if got := res.Files["index.html"]; got != "[a][b]" {
		t.Fatalf("output = %q; want %q", got, "[a][b]")
	}
}

// TestBarePropUndefinedVsPassed: a bare prop that is not passed is UNDEFINED (a
// plain echo diagnoses undefined-variable); passing it defines the variable.
func TestBarePropUndefinedVsPassed(t *testing.T) {
	// not passed → undefined
	res := compileMem(t, map[string]string{
		pageEntry:                                `[<x-c/>]`,
		"resources/views/components/c.blade.php": `@props(['bare'])<{{ $bare }}>`,
	}, nil)
	if got := res.Files["index.html"]; got != "[<>]" {
		t.Fatalf("unpassed output = %q; want %q", got, "[<>]")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Construct != "undefined-variable" {
		t.Fatalf("unpassed diagnostics = %v; want one undefined-variable", res.Diagnostics)
	}
	// passed → defined
	res2 := compileMem(t, map[string]string{
		pageEntry:                                `<x-c bare="V"/>`,
		"resources/views/components/c.blade.php": `@props(['bare'])<{{ $bare }}>`,
	}, nil)
	if got := res2.Files["index.html"]; got != "<V>" {
		t.Fatalf("passed output = %q; want %q", got, "<V>")
	}
	if len(res2.Diagnostics) != 0 {
		t.Fatalf("passed diagnostics = %v; want none", res2.Diagnostics)
	}
}

// TestPropsNotFirst: @props that is not the first non-whitespace node is a
// props error and is ignored (defaults not applied).
func TestPropsNotFirst(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `<x-c/>`,
		// '>' (non-word) before @props gives it a word boundary while keeping it
		// off the first node, so the not-first props error still fires.
		"resources/views/components/c.blade.php": `>@props(['a' => 'AA'])[{{ $a ?? 'z' }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != ">[z]" {
		t.Fatalf("output = %q; want %q", got, ">[z]")
	}
	if !hasConstruct(res.Diagnostics, "props") {
		t.Fatalf("diagnostics = %v; want a props error", res.Diagnostics)
	}
}

// TestPropsOnPage: @props is only legal in a component file; on the main page it
// is a props error and ignored (a deliberate divergence from real Blade, which
// applies page-level defaults — documented in the task report).
func TestPropsOnPage(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `@props(['a' => 'AA'])X{{ $a ?? 'z' }}Y`,
	}, nil)
	if got := res.Files["index.html"]; got != "XzY" {
		t.Fatalf("output = %q; want %q", got, "XzY")
	}
	if !hasConstruct(res.Diagnostics, "props") {
		t.Fatalf("diagnostics = %v; want a props error", res.Diagnostics)
	}
}

// TestMalformedProps: an illegal @props literal diagnoses props and declares
// nothing — but the passed attribute still binds, because attributes reach an
// anonymous component as view data independently of the @props line (real
// Blade binds $a before compileProps runs; see buildComponentScope).
func TestMalformedProps(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry:                                `<x-c a="x"/>`,
		"resources/views/components/c.blade.php": `@props('not an array')[{{ $a ?? 'none' }}]`,
	}, nil)
	if got := res.Files["index.html"]; got != "[x]" {
		t.Fatalf("output = %q; want %q", got, "[x]")
	}
	if !hasConstruct(res.Diagnostics, "props") {
		t.Fatalf("diagnostics = %v; want a props error", res.Diagnostics)
	}
}

// TestComponentTagEatsNewline: a component tag eats one following newline, like
// a block directive.
func TestComponentTagEatsNewline(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: "A\n<x-mk/>\nB\n<x-pair>x</x-pair>\nC",
		"resources/views/components/mk.blade.php":   `<M>`,
		"resources/views/components/pair.blade.php": `<P>{{ $slot }}</P>`,
	}, nil)
	if got := res.Files["index.html"]; got != "A\n<M>B\n<P>x</P>C" {
		t.Fatalf("output = %q; want %q", got, "A\n<M>B\n<P>x</P>C")
	}
}

func hasConstruct(ds []Diagnostic, construct string) bool {
	for _, d := range ds {
		if d.Construct == construct {
			return true
		}
	}
	return false
}
