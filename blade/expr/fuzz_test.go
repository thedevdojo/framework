package expr

import "testing"

// FuzzEval asserts Eval and RequiredVars never panic on arbitrary input. The
// only invariant is that they always return; any error is acceptable.
func FuzzEval(f *testing.F) {
	seeds := []string{
		`$site->name`,
		`$plans[0]['price']`,
		`$missing ?? 'd'`,
		`$a == $b && $c || !$d`,
		`null ?? false ?? 'x'`,
		`1 < 2 < 3`,
		`count($x)`,
		`$a->b->c ?? 'z'`,
		`-$n`,
		`(($a ?? 1) && $b)`,
		`'it\'s a "test"'`,
		`$a ?->b`,
		`$$x`,
		`Foo::BAR`,
		`$a . $b + 1`,
		`[`,
		`]]]`,
		`??`,
		`$`,
		`'unterminated`,
		`   `,
		``,
		`1.2.3`,
		`$a[$b[$c]]`,
		`true==null`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	scope := mapScope{
		"a":     int64(1),
		"b":     "2",
		"c":     nil,
		"site":  map[string]Value{"name": "X"},
		"plans": []Value{map[string]Value{"price": int64(9)}},
	}

	f.Fuzz(func(t *testing.T, src string) {
		v, err := Eval(src, scope) // must not panic
		if err == nil {
			switch v.(type) {
			case nil, bool, int64, float64, string, []Value, map[string]Value, *Object:
				// in-domain
			default:
				t.Fatalf("Eval(%q) returned out-of-domain value %T", src, v)
			}
		}
		if names, err := RequiredVars(src); err == nil { // must not panic
			for _, n := range names {
				if n == "" {
					t.Fatalf("RequiredVars(%q) returned an empty name", src)
				}
			}
		}
	})
}
