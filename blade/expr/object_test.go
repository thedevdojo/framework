package expr

import (
	"errors"
	"reflect"
	"testing"
)

// objScope builds a scope holding a stdClass-like *Object ($site) plus a couple
// of nested objects for equality tests, mirroring the SiteData shape.
func objScope() mapScope {
	mid := NewObject()
	mid.Set("inner", "I")
	mid2 := NewObject() // same props/values as mid, DIFFERENT insertion order below
	mid2.Set("inner", "I")
	midDiff := NewObject()
	midDiff.Set("inner", "J")

	site := NewObject()
	site.Set("alpha", "A")
	site.Set("num", int64(5))
	site.Set("flag", true)
	site.Set("mid", mid)
	site.Set("mid2", mid2)
	site.Set("midDiff", midDiff)
	site.Set("empty", NewObject())
	site.Set("list", []Value{int64(1), int64(2)})

	return mapScope{"site": site}
}

// TestObjectTruthy: a stdClass is ALWAYS truthy, even empty — the flip that
// gives `@if ($site)` parity for an empty site.json.
func TestObjectTruthy(t *testing.T) {
	if !Truthy(NewObject()) {
		t.Errorf("Truthy(empty *Object) = false; want true")
	}
	o := NewObject()
	o.Set("a", int64(1))
	if !Truthy(o) {
		t.Errorf("Truthy(non-empty *Object) = false; want true")
	}
}

// TestObjectPropAccess: present member reads; a missing member is
// ErrUndefinedProp; `??` suppresses the missing member.
func TestObjectPropAccess(t *testing.T) {
	scope := objScope()

	if got, err := Eval("$site->alpha", scope); err != nil || got != "A" {
		t.Errorf("$site->alpha = %v, %v; want A, nil", got, err)
	}

	_, err := Eval("$site->nope", scope)
	if !errors.Is(err, ErrUndefinedProp) {
		t.Errorf("$site->nope err = %v; want ErrUndefinedProp", err)
	}
	var up *UndefinedPropError
	if !errors.As(err, &up) || up.Name != "nope" {
		t.Errorf("$site->nope error type/name = %v; want UndefinedPropError{nope}", err)
	}

	if got, err := Eval("$site->nope ?? 'd'", scope); err != nil || got != "d" {
		t.Errorf("$site->nope ?? 'd' = %v, %v; want d, nil (?? suppresses)", got, err)
	}
}

// TestObjectIndexAccess: `[]` on an object is ErrObjectAsArray, and `??` does
// NOT suppress it (oracle-probed: `$obj['k'] ?? 'd'` still fatals).
func TestObjectIndexAccess(t *testing.T) {
	scope := objScope()

	_, err := Eval("$site['alpha']", scope)
	if !errors.Is(err, ErrObjectAsArray) {
		t.Errorf("$site['alpha'] err = %v; want ErrObjectAsArray", err)
	}

	_, err = Eval("$site['alpha'] ?? 'd'", scope)
	if !errors.Is(err, ErrObjectAsArray) {
		t.Errorf("$site['alpha'] ?? 'd' err = %v; want ErrObjectAsArray (?? does NOT suppress)", err)
	}
}

// TestObjectLooseEq: null/bool/string/array comparisons resolve to a bool;
// object-vs-object is order-independent, recursive; object-vs-number errors.
func TestObjectLooseEq(t *testing.T) {
	scope := objScope()
	truthyCases := []struct {
		src  string
		want bool
	}{
		{"$site->mid == null", false},
		{"$site->empty == null", false},
		{"$site->mid == true", true},
		{"$site->mid == false", false},
		{"$site->empty == true", true},
		{"$site->mid == $site->mid2", true},     // same props, order-independent
		{"$site->mid == $site->midDiff", false}, // value differs
		{"$site->mid == 'x'", false},            // object vs string: false, no fatal
		{"$site->mid == $site->list", false},    // object vs array: false
		{"$site->mid != $site->midDiff", true},
	}
	for _, c := range truthyCases {
		got, err := Eval(c.src, scope)
		if err != nil {
			t.Errorf("%s errored: %v", c.src, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %v; want %v", c.src, got, c.want)
		}
	}

	// object vs a number fatals in PHP; the engine surfaces an error.
	for _, src := range []string{"$site->mid == 5", "$site->num == $site->mid", "$site->mid != 5"} {
		if _, err := Eval(src, scope); err == nil {
			t.Errorf("%s = nil error; want an object-vs-number error", src)
		}
	}
}

// TestObjectOrderCompare: an ordered comparison touching an object is out of
// v1's subset — a plain error @if treats as false.
func TestObjectOrderCompare(t *testing.T) {
	scope := objScope()
	for _, src := range []string{"$site->mid < $site->midDiff", "$site->num > $site->mid"} {
		if _, err := Eval(src, scope); err == nil {
			t.Errorf("%s = nil error; want errObjectOrder", src)
		}
	}
}

// TestDecodeJSONOrder: objects preserve document (insertion) order, arrays keep
// element order, nested objects become *Object, numbers stay float64.
func TestDecodeJSONOrder(t *testing.T) {
	v, err := DecodeJSON([]byte(`{"zebra":1,"apple":2,"mango":{"x":true},"list":[3,4]}`))
	if err != nil {
		t.Fatalf("DecodeJSON error: %v", err)
	}
	obj, ok := v.(*Object)
	if !ok {
		t.Fatalf("top-level = %T; want *Object", v)
	}
	if got := obj.Keys(); !reflect.DeepEqual(got, []string{"zebra", "apple", "mango", "list"}) {
		t.Errorf("keys = %v; want insertion order zebra,apple,mango,list", got)
	}
	if z, _ := obj.Get("zebra"); z != float64(1) {
		t.Errorf("zebra = %#v; want float64(1)", z)
	}
	nested, _ := obj.Get("mango")
	if _, ok := nested.(*Object); !ok {
		t.Errorf("nested object = %T; want *Object", nested)
	}
	list, _ := obj.Get("list")
	if _, ok := list.([]Value); !ok {
		t.Errorf("list = %T; want []Value", list)
	}
}

// TestDecodeJSONInvalid: malformed input, a non-object top level, and trailing
// data are all handled without panic (callers decide what to do with each).
func TestDecodeJSONInvalid(t *testing.T) {
	if _, err := DecodeJSON([]byte(`{ not json`)); err == nil {
		t.Errorf("malformed JSON: want error")
	}
	v, err := DecodeJSON([]byte(`["a","b"]`))
	if err != nil {
		t.Errorf("top-level array: unexpected error %v", err)
	}
	if _, ok := v.([]Value); !ok {
		t.Errorf("top-level array decoded to %T; want []Value", v)
	}
	if _, err := DecodeJSON([]byte(`{"a":1} 2`)); err == nil {
		t.Errorf("trailing data: want error")
	}
}

// TestObjectStringifyNotStringable: an object cannot be echoed — Eval returns
// the value fine, but the render tier refuses it (see Stringify). Here we assert
// the value is an *Object so the render path hits its errNotStringable branch.
func TestObjectAppTierMember(t *testing.T) {
	loop := NewObject()
	loop.Set("index", int64(0))
	loop.MarkAppTier("remaining", "depth", "parent")

	scope := mapScope{"loop": loop}

	if got, err := Eval("$loop->index", scope); err != nil || got != int64(0) {
		t.Errorf("$loop->index = %v, %v; want 0, nil", got, err)
	}

	_, err := Eval("$loop->remaining", scope)
	var se *SyntaxError
	if !errors.As(err, &se) || !se.AppTier {
		t.Errorf("$loop->remaining err = %v; want app-tier SyntaxError", err)
	}

	// A member that is neither present nor app-tier is a normal undefined prop.
	_, err = Eval("$loop->typo", scope)
	if !errors.Is(err, ErrUndefinedProp) {
		t.Errorf("$loop->typo err = %v; want ErrUndefinedProp", err)
	}
}
