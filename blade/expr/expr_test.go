package expr

import (
	"errors"
	"reflect"
	"testing"
)

// mapScope is a trivial Scope backed by a Go map. A key present with a nil
// value models a defined-but-null variable (Lookup returns ok=true).
type mapScope map[string]Value

func (m mapScope) Lookup(name string) (Value, bool) {
	v, ok := m[name]
	return v, ok
}

// briefScope mirrors the scope from the task brief plus a couple of extra
// bindings the access-chain rows need (a present-but-nil var and no others).
func briefScope() mapScope {
	return mapScope{
		"plans": []Value{
			map[string]Value{"name": "Pro", "price": int64(9)},
		},
		"site": map[string]Value{"name": "X"},
		"zero": int64(0),
		"nada": nil, // present, value nil
	}
}

// ---------------------------------------------------------------------------
// Eval value rows
// ---------------------------------------------------------------------------

func TestEvalValues(t *testing.T) {
	scope := briefScope()
	cases := []struct {
		src  string
		want Value
	}{
		// --- brief truth-table rows (values) ---
		{`$site->name`, "X"},
		{`$plans[0]['price']`, int64(9)},
		{`$missing ?? 'd'`, "d"},
		{`$site->nope ?? 'd'`, "d"},
		{`$zero == '0'`, true}, // PHP loose equality
		{`'1' == 1`, true},
		{`'abc' == 0`, false}, // PHP 8 semantics
		{`1 < 2`, true},
		{`'a' < 'b'`, true}, // byte-order string compare
		{`!$zero`, true},
		{`$zero || true`, true},
		{`null ?? false ?? 'x'`, false},

		// CONFLICT: the brief lists {`'10' < '9'`, true} with the comment
		// "both numeric-ish -> numeric compare". Numeric compare of 10 and 9
		// makes '10' < '9' FALSE (matching PHP 8: var_dump('10' < '9') => false).
		// The brief's expected `true` is the byte-order answer and contradicts
		// its own comment. Per the task's "semantics win" rule we assert the
		// correct PHP value. See the report for details.
		{`'10' < '9'`, false},
		// Correct demonstrations of numeric-string ordering:
		{`'9' < '10'`, true},
		{`'10' > '9'`, true},

		// --- literals ---
		{`5`, int64(5)},
		{`1.5`, float64(1.5)},
		{`'hi'`, "hi"},
		{`"dq"`, "dq"},
		{`true`, true},
		{`false`, false},
		{`TRUE`, true}, // keywords are case-insensitive
		{`Null`, nil},  // ^
		{`null`, nil},
		{`-5`, int64(-5)},
		{`- 5`, int64(-5)},
		{`-1.5`, float64(-1.5)},
		{`-$zero`, int64(0)},

		// --- string escapes ---
		{`'it\'s'`, "it's"},
		{`'a\\b'`, `a\b`},
		{`"he said \"hi\""`, `he said "hi"`},
		{`"$x is literal"`, "$x is literal"}, // no interpolation in double quotes

		// --- access chains ---
		{`$plans[0]['name']`, "Pro"},
		{`$plans[0.0]['name']`, "Pro"},     // float index coerces to int
		{`$plans['0']['price']`, int64(9)}, // numeric-string index coerces
		{`$site->nope`, nil},               // missing map key -> nil, no error
		{`$nada->x`, nil},                  // property on nil -> nil
		{`$zero->x`, nil},                  // property on scalar -> nil
		{`$zero['k']`, nil},                // index on scalar -> nil
		{`$plans[9]`, nil},                 // out of range -> nil, no error
		{`$plans[5] ?? 'none'`, "none"},    // out of range -> nil -> coalesce
		{`$plans[-1] ?? 'none'`, "none"},   // negative index -> nil -> coalesce

		// --- loose equality (PHP 8) ---
		{`0 == ''`, false},   // empty string is not numeric
		{`null == ''`, true}, // null vs "" -> true
		{`null == '0'`, false},
		{`null == 0`, true},
		{`null == null`, true},
		{`null == false`, true},
		{`1 == 1.0`, true}, // int vs float numeric
		{`'1.0' == '1'`, true},
		{`'10' == '9'`, false},
		{`'abc' == 'abc'`, true},
		{`true == 1`, true},
		{`false == 0`, true},
		{`false == ''`, true},
		{`true == 'abc'`, true},
		{`true == '0'`, false}, // "0" is falsy in PHP
		{`1 != 2`, true},
		{`'abc' != 0`, true},

		// --- ordered comparison ---
		{`2 <= 2`, true},
		{`3 >= 4`, false},
		{`3 > 2`, true},
		{`2 < 1`, false},

		// --- logical / coalesce ---
		{`$zero && true`, false},
		{`true && $site->name`, true}, // && yields a bool, not the string
		{`false || $zero`, false},
		{`!$site`, false}, // non-empty map is truthy
		{`!!'x'`, true},
		{`$missing ?? $site->name`, "X"},
		{`null ?? null ?? 'last'`, "last"},
		{`$nada ?? 'fallback'`, "fallback"}, // present-but-nil triggers ??

		// Short-circuit: a false left operand of && must NOT evaluate the right,
		// so an undefined $undef raises no error — the value is plain false.
		{`false && $undef`, false},
	}

	for _, c := range cases {
		got, err := Eval(c.src, scope)
		if err != nil {
			t.Errorf("Eval(%q) unexpected error: %v", c.src, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Eval(%q) = %#v (%T), want %#v (%T)", c.src, got, got, c.want, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Eval error rows
// ---------------------------------------------------------------------------

func syntaxErr(t *testing.T, err error) *SyntaxError {
	t.Helper()
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SyntaxError, got %T: %v", err, err)
	}
	return se
}

func TestEvalErrors(t *testing.T) {
	scope := briefScope()

	t.Run("undefined variable", func(t *testing.T) {
		_, err := Eval(`$missing`, scope)
		if !errors.Is(err, ErrUndefinedVar) {
			t.Fatalf("expected ErrUndefinedVar, got %v", err)
		}
		var uv *UndefinedVarError
		if !errors.As(err, &uv) {
			t.Fatalf("expected *UndefinedVarError, got %T: %v", err, err)
		}
		if uv.Name != "missing" {
			t.Fatalf("UndefinedVarError.Name = %q; want %q", uv.Name, "missing")
		}
	})

	// The renderer's `$attributes` app-tier carve-out (Task 11) relies on
	// pulling the variable name out of an undefined-variable error via
	// errors.As, not on parsing Error()'s text.
	t.Run("undefined variable name for attributes", func(t *testing.T) {
		_, err := Eval(`$attributes`, scope)
		var uv *UndefinedVarError
		if !errors.As(err, &uv) || uv.Name != "attributes" {
			t.Fatalf("expected UndefinedVarError{Name: %q}, got %#v (%v)", "attributes", err, err)
		}
	})

	// AppTier=true: valid Blade/PHP the fast path does not model.
	appTier := []string{
		`count($x)`,
		`$obj->method()`,
		`$obj?->prop`,
		`$a . $b`,
		`$a + 1`,
		`$a - 1`, // binary minus (unary is supported)
		`$a * 2`,
		`$a / 2`,
		`$a % 2`,
		`$a = 1`,
		`$a += 1`,
		`$a === $b`,
		`$a <=> $b`,
		`Foo::BAR`,
		`PHP_EOL`,
		`$$x`,
		`$a and $b`, // word operator
	}
	for _, src := range appTier {
		t.Run("apptier "+src, func(t *testing.T) {
			_, err := Eval(src, scope)
			se := syntaxErr(t, err)
			if !se.AppTier {
				t.Fatalf("Eval(%q): expected AppTier=true, got %v", src, se)
			}
		})
	}

	// AppTier=false: malformed input the renderer should surface as an error.
	garbage := []string{
		`1 < 2 < 3`, // non-associative comparison
		`(1`,        // unbalanced
		`1)`,
		`#`,
		`'unterminated`,
		``, // empty expression
		`1 2`,
	}
	for _, src := range garbage {
		t.Run("garbage "+src, func(t *testing.T) {
			_, err := Eval(src, scope)
			se := syntaxErr(t, err)
			if se.AppTier {
				t.Fatalf("Eval(%q): expected AppTier=false, got %v", src, se)
			}
		})
	}

	// Ordered comparison involving an array is a plain eval error: not
	// ErrUndefinedVar, not a *SyntaxError.
	arrayOrder := []string{`$plans < 1`, `$plans <= $plans`, `1 > $plans`}
	for _, src := range arrayOrder {
		t.Run("array order "+src, func(t *testing.T) {
			_, err := Eval(src, scope)
			if err == nil {
				t.Fatalf("Eval(%q): expected error", src)
			}
			if errors.Is(err, ErrUndefinedVar) {
				t.Fatalf("Eval(%q): should not be ErrUndefinedVar", src)
			}
			var se *SyntaxError
			if errors.As(err, &se) {
				t.Fatalf("Eval(%q): should be a plain error, got *SyntaxError", src)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Truthy
// ---------------------------------------------------------------------------

func TestTruthy(t *testing.T) {
	cases := []struct {
		v    Value
		want bool
	}{
		// falsy
		{false, false},
		{int64(0), false},
		{float64(0), false},
		{"", false},
		{"0", false},
		{nil, false},
		{[]Value{}, false},
		{map[string]Value{}, false},
		// truthy
		{true, true},
		{int64(1), true},
		{int64(-1), true},
		{float64(-0.5), true},
		{"false", true},
		{"00", true},
		{" ", true},
		{"hi", true},
		{[]Value{int64(1)}, true},
		{map[string]Value{"a": int64(1)}, true},
	}
	for _, c := range cases {
		if got := Truthy(c.v); got != c.want {
			t.Errorf("Truthy(%#v) = %v, want %v", c.v, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// LooseEq (PHP 8)
// ---------------------------------------------------------------------------

func TestLooseEq(t *testing.T) {
	cases := []struct {
		a, b Value
		want bool
	}{
		// null
		{nil, nil, true},
		{nil, false, true},
		{nil, true, false},
		{nil, int64(0), true},
		{nil, int64(1), false},
		{nil, float64(0), true},
		{nil, "", true},
		{nil, "0", false},
		{nil, []Value{}, true},
		{nil, []Value{int64(1)}, false},
		// numeric vs numeric-string
		{int64(0), "0", true},
		{"1", int64(1), true},
		{"abc", int64(0), false},
		{int64(0), "", false},
		{int64(1), float64(1), true},
		// string vs string
		{"1.0", "1", true},
		{"10", "9", false},
		{"abc", "abc", true},
		{"x", "y", false},
		// bool coercion
		{true, int64(1), true},
		{false, int64(0), true},
		{false, "", true},
		{true, "abc", true},
		{true, "0", false},
		{"0", false, true},
		{false, []Value{}, true}, // bool coercion: Truthy(false) == Truthy([]) == false
		// arrays
		{[]Value{int64(1)}, []Value{int64(1)}, true},
		{[]Value{int64(1)}, []Value{int64(2)}, false},
		{map[string]Value{"a": int64(1)}, map[string]Value{"a": int64(1)}, true},
		{map[string]Value{"a": int64(1)}, []Value{int64(1)}, false},
		// number vs number
		{int64(5), int64(5), true},
		{int64(5), int64(6), false},
	}
	for _, c := range cases {
		if got := LooseEq(c.a, c.b); got != c.want {
			t.Errorf("LooseEq(%#v, %#v) = %v, want %v", c.a, c.b, got, c.want)
		}
		// symmetric
		if got := LooseEq(c.b, c.a); got != c.want {
			t.Errorf("LooseEq(%#v, %#v) [swapped] = %v, want %v", c.b, c.a, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// compareOrder (white-box)
// ---------------------------------------------------------------------------

func TestCompareOrder(t *testing.T) {
	cases := []struct {
		a, b Value
		want int
	}{
		{int64(1), int64(2), -1},
		{int64(2), int64(2), 0},
		{int64(3), int64(2), 1},
		{"9", "10", -1}, // both numeric strings -> numeric
		{"10", "9", 1},  // ^
		{"a", "b", -1},  // byte-order
		{"b", "a", 1},
		{int64(10), "9", 1},   // number vs numeric string -> numeric
		{int64(1), "abc", -1}, // number vs non-numeric string -> "1" < "abc"
		{float64(1.5), int64(2), -1},
	}
	for _, c := range cases {
		got, err := compareOrder(c.a, c.b)
		if err != nil {
			t.Errorf("compareOrder(%#v, %#v) unexpected error: %v", c.a, c.b, err)
			continue
		}
		if sign(got) != c.want {
			t.Errorf("compareOrder(%#v, %#v) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}

	if _, err := compareOrder([]Value{int64(1)}, int64(1)); err == nil {
		t.Errorf("compareOrder(array, scalar): expected error")
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// RequiredVars
// ---------------------------------------------------------------------------

func TestRequiredVars(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{`$a ?? 'd'`, []string{}},
		{`$a->b ?? 'd'`, []string{}},
		{`$a == $b`, []string{"a", "b"}},
		{`$a ?? $b`, []string{"b"}},
		{`($a ?? 1) && $c`, []string{"c"}},
		{`!$x`, []string{"x"}},
		{`$a[$b]`, []string{"a", "b"}},
		{`$missing ?? $site->name`, []string{"site"}},
		{`$a && $b || $c`, []string{"a", "b", "c"}},
		{`$plans[0]['price']`, []string{"plans"}},
		{`$a ?? $a`, []string{"a"}},     // fallback reference is required
		{`$x == $y ?? 'd'`, []string{}}, // (x==y) is the protected LHS
		{`'literal'`, []string{}},
	}
	for _, c := range cases {
		got, err := RequiredVars(c.src)
		if err != nil {
			t.Errorf("RequiredVars(%q) unexpected error: %v", c.src, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("RequiredVars(%q) = %#v, want %#v", c.src, got, c.want)
		}
	}
}

func TestRequiredVarsError(t *testing.T) {
	if _, err := RequiredVars(`count($x)`); err == nil {
		t.Fatalf("expected error for app-tier source")
	}
	if _, err := RequiredVars(`(1`); err == nil {
		t.Fatalf("expected error for malformed source")
	}
}
