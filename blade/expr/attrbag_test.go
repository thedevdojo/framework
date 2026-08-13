package expr

import (
	"errors"
	"testing"
)

// bagOf builds an AttrBag from alternating name/value pairs, in order.
func bagOf(pairs ...any) *AttrBag {
	var entries []AttrEntry
	for i := 0; i+1 < len(pairs); i += 2 {
		entries = append(entries, AttrEntry{Name: pairs[i].(string), Val: pairs[i+1]})
	}
	return NewAttrBag(entries)
}

func bagScope(b *AttrBag) mapScope { return mapScope{"attributes": b} }

// htmlOf renders a Value that must be an *AttrBag to its HTML string, failing
// the test on type or stringification errors.
func htmlOf(t *testing.T, v Value) string {
	t.Helper()
	b, ok := v.(*AttrBag)
	if !ok {
		t.Fatalf("value is %T, want *AttrBag", v)
	}
	s, err := b.HTMLString()
	if err != nil {
		t.Fatalf("HTMLString: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// __toString / HTMLString
// ---------------------------------------------------------------------------

func TestAttrBagHTMLString(t *testing.T) {
	cases := []struct {
		name string
		bag  *AttrBag
		want string
	}{
		{"basic", bagOf("class", "p-4", "id", "hero"), `class="p-4" id="hero"`},
		{"skips_false_and_null", bagOf("a", false, "b", nil, "c", "x"), `c="x"`},
		{"true_becomes_key", bagOf("disabled", true), `disabled="disabled"`},
		{"true_xdata_empty", bagOf("x-data", true), `x-data=""`},
		{"true_wire_prefix_empty", bagOf("wire:poll", true), `wire:poll=""`},
		{"value_trimmed", bagOf("title", "  hi  "), `title="hi"`},
		{"quote_backslash_escaped", bagOf("t", `a"b`), `t="a\"b"`},
		{"int_value", bagOf("n", int64(5)), `n="5"`},
		{"empty_bag", bagOf(), ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.bag.HTMLString()
			if err != nil {
				t.Fatalf("HTMLString: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestAttrBagHTMLStringNonScalarErrors(t *testing.T) {
	arr := NewArr()
	arr.Push("x")
	if _, err := bagOf("bad", arr).HTMLString(); err == nil {
		t.Errorf("array-valued attribute should not stringify")
	}
}

// setAttributes' special case: an entry literally named `attributes` holding a
// parent bag folds that bag in via parent.merge(others, escape=false).
func TestAttrBagParentBagPassThrough(t *testing.T) {
	parent := bagOf("class", "a", "id", "i")
	b := NewAttrBag([]AttrEntry{
		{Name: "attributes", Val: parent},
		{Name: "class", Val: "b"},
	})
	s, err := b.HTMLString()
	if err != nil {
		t.Fatalf("HTMLString: %v", err)
	}
	if want := `class="b a" id="i"`; s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}

// ---------------------------------------------------------------------------
// Array literals in the expression language
// ---------------------------------------------------------------------------

func TestArrayLiterals(t *testing.T) {
	scope := mapScope{"v": int64(3)}
	cases := []struct {
		src  string
		want Value
	}{
		{`['a', 'b'][1]`, "b"},
		{`['x' => 1, 'y' => 2]['y']`, int64(2)},
		{`['k' => $v]['k']`, int64(3)},
		{`[1 => 'a', 'b'][2]`, "b"},          // auto-index continues after explicit key
		{`['5' => 'a'][5]`, "a"},             // canonical numeric-string key casts to int
		{`['05' => 'a']['05']`, "a"},         // non-canonical stays a string key
		{`['a',][0]`, "a"},                   // trailing comma tolerated
		{`![]`, true},                        // empty array is falsy
		{`!['a']`, false},                    // non-empty array is truthy
		{`['a' => 1] == ['a' => '1']`, true}, // loose equality, PHP ==
		{`['a'] == ['b']`, false},
		{`[] == []`, true},
		{`[[1, 2][1], 'z'][0]`, int64(2)}, // nesting
		{`['m' => 'x']['missing'] ?? 'd'`, "d"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(c.src, scope)
			if err != nil {
				t.Fatalf("Eval(%q): %v", c.src, err)
			}
			if !LooseEq(got, c.want) || (got == nil) != (c.want == nil) {
				t.Errorf("Eval(%q) = %#v, want %#v", c.src, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Bag methods through Eval
// ---------------------------------------------------------------------------

func TestAttrBagMethodsEval(t *testing.T) {
	b := bagOf("class", "p-4", "id", "hero", "data-null", nil, "wire:model", "name")
	scope := bagScope(b)
	scope["err"] = true

	boolCases := []struct {
		src  string
		want bool
	}{
		{`$attributes->has('class')`, true},
		{`$attributes->has('nope')`, false},
		{`$attributes->has('data-null')`, true}, // present-with-null still counts
		{`$attributes->has('class', 'id')`, true},
		{`$attributes->has('class', 'nope')`, false},
		{`$attributes->has(['class', 'id'])`, true},
		{`$attributes->hasAny('nope', 'id')`, true},
		{`$attributes->hasAny('nope', 'nada')`, false},
		{`$attributes->missing('nope')`, true},
		{`$attributes->missing('class')`, false},
		{`$attributes->isEmpty()`, false},
		{`$attributes->isNotEmpty()`, true},
	}
	for _, c := range boolCases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(c.src, scope)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got != c.want {
				t.Errorf("got %#v, want %v", got, c.want)
			}
		})
	}

	valCases := []struct {
		src  string
		want Value
	}{
		{`$attributes->get('class')`, "p-4"},
		{`$attributes->get('nope')`, nil},
		{`$attributes->get('nope', 'fallback')`, "fallback"},
		{`$attributes->first()`, "p-4"},
	}
	for _, c := range valCases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(c.src, scope)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got != c.want {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}

	htmlCases := []struct {
		src  string
		want string
	}{
		{`$attributes->only('class')`, `class="p-4"`},
		{`$attributes->only(['class', 'id'])`, `class="p-4" id="hero"`},
		{`$attributes->except(['class', 'data-null', 'wire:model'])`, `id="hero"`},
		{`$attributes->whereStartsWith('wire:')`, `wire:model="name"`},
		{`$attributes->whereDoesntStartWith('wire:')`, `class="p-4" id="hero"`},
		{`$attributes->thatStartWith('wire:')`, `wire:model="name"`},
		{`$attributes->merge(['type' => 'button', 'class' => 'font-bold'])`, `type="button" class="font-bold p-4" id="hero" wire:model="name"`},
		{`$attributes->merge()`, `class="p-4" id="hero" wire:model="name"`},
		{`$attributes->merge(['title' => 'a<b'])`, `title="a&lt;b" class="p-4" id="hero" wire:model="name"`},
		{`$attributes->class(['font-bold', 'text-red' => $err, 'hidden' => false])`, `class="font-bold text-red p-4" id="hero" wire:model="name"`},
		{`$attributes->class('shadow')`, `class="shadow p-4" id="hero" wire:model="name"`},
	}
	for _, c := range htmlCases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(c.src, scope)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if s := htmlOf(t, got); s != c.want {
				t.Errorf("got %q, want %q", s, c.want)
			}
		})
	}
}

func TestAttrBagStyleMerge(t *testing.T) {
	b := bagOf("style", "color: red")
	got, err := Eval(`$attributes->style(['font-weight: bold' => true, 'display: none' => false])`, bagScope(b))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if s := htmlOf(t, got); s != `style="font-weight: bold; color: red;"` {
		t.Errorf("got %q", s)
	}
}

// Chained calls: the result of one bag method accepts another.
func TestAttrBagChaining(t *testing.T) {
	b := bagOf("class", "p-4", "id", "x")
	got, err := Eval(`$attributes->except('id')->merge(['class' => 'm-2'])`, bagScope(b))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if s := htmlOf(t, got); s != `class="m-2 p-4"` {
		t.Errorf("got %q", s)
	}
}

// ---------------------------------------------------------------------------
// App-tier classification for calls the engine cannot model
// ---------------------------------------------------------------------------

func TestMethodCallAppTier(t *testing.T) {
	b := bagOf("class", "p-4")
	obj := NewObject()
	scope := mapScope{"attributes": b, "obj": obj, "fn": nil}

	cases := []string{
		`$obj->fetch()`,            // method call on a non-bag object
		`$undefined->fetch()`,      // even an undefined receiver stays app-tier
		`$attributes->filter($fn)`, // closure-taking bag method
		`$attributes->prepends('x')`,
		`$attributes->nonsense()`, // unknown method (bag is Macroable in PHP)
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			_, err := Eval(src, scope)
			var se *SyntaxError
			if !errors.As(err, &se) || !se.AppTier {
				t.Errorf("Eval(%q) err = %v; want app-tier SyntaxError", src, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CSS class / style list compilation (shared with the @class/@style directives)
// ---------------------------------------------------------------------------

func TestToCssClasses(t *testing.T) {
	arr := NewArr()
	arr.Push("p-4")
	arr.SetKey("font-bold", true)
	arr.SetKey("hidden", false)
	arr.Push("mt-2")
	got, err := ToCssClasses(arr)
	if err != nil {
		t.Fatalf("ToCssClasses: %v", err)
	}
	if want := "p-4 font-bold mt-2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Arr::wrap: a bare string is a single class; nil is empty.
	if got, _ := ToCssClasses("solo"); got != "solo" {
		t.Errorf("wrap(string): got %q", got)
	}
	if got, _ := ToCssClasses(nil); got != "" {
		t.Errorf("wrap(nil): got %q", got)
	}
}

func TestToCssStyles(t *testing.T) {
	arr := NewArr()
	arr.Push("color: red")
	arr.SetKey("font-weight: bold;", true)
	arr.SetKey("display: none", false)
	got, err := ToCssStyles(arr)
	if err != nil {
		t.Fatalf("ToCssStyles: %v", err)
	}
	if want := "color: red; font-weight: bold;"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
