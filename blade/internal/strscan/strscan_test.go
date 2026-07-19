package strscan

import (
	"reflect"
	"testing"
)

func TestIndex(t *testing.T) {
	cases := []struct {
		s, tok string
		want   int
	}{
		{`a => b`, "=>", 2},
		{`a => b => c`, "=>", 2},      // first occurrence
		{`['a' => 1] => b`, "=>", 11}, // bracketed arrow is not top-level
		{`('=>') => b`, "=>", 7},      // quoted arrow is not a match
		{`"x => y"`, "=>", -1},        // fully quoted: no match
		{`'\' => ' => b`, "=>", 9},    // escaped quote stays inside the string
		{`(a => b)`, "=>", -1},        // nested only
		{`a =`, "=>", -1},             // token must fit
		{`=>`, "=>", 0},               // match at start
		{`a)b => c`, "=>", 4},         // stray closer clamps: still top-level
		{``, "=>", -1},
	}
	for _, c := range cases {
		if got := Index(c.s, c.tok); got != c.want {
			t.Errorf("Index(%q, %q) = %d; want %d", c.s, c.tok, got, c.want)
		}
	}
}

func TestLastIndex(t *testing.T) {
	cases := []struct {
		s, tok string
		want   int
	}{
		{`$a as $b`, " as ", 2},
		{`$a as $b as $c`, " as ", 8},        // LAST occurrence
		{`func($x as $y) as $z`, " as ", 14}, // bracketed " as " skipped
		{`'$x as $y' as $z`, " as ", 10},     // quoted " as " skipped
		{`$a as`, " as ", -1},                // trailing space missing: no fit
		{`$items as $item`, " as ", 6},
		{`$a AS $b`, " as ", -1}, // case-sensitive
		{``, " as ", -1},
	}
	for _, c := range cases {
		if got := LastIndex(c.s, c.tok); got != c.want {
			t.Errorf("LastIndex(%q, %q) = %d; want %d", c.s, c.tok, got, c.want)
		}
	}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		s        string
		want     []string
		balanced bool
	}{
		{`a, b, c`, []string{"a", " b", " c"}, true},
		{``, []string{""}, true},
		{`a`, []string{"a"}, true},
		{`a,`, []string{"a", ""}, true},                   // trailing separator: empty final part
		{`'a,b', c`, []string{"'a,b'", " c"}, true},       // quoted separator not split
		{`[1, 2], [3]`, []string{"[1, 2]", " [3]"}, true}, // bracketed separators not split
		{`f(a, b), c`, []string{"f(a, b)", " c"}, true},   // parens too
		{`'it\'s', b`, []string{`'it\'s'`, " b"}, true},   // escaped quote inside string
		{`'open`, []string{"'open"}, false},               // unterminated quote
		{`[a, b`, []string{"[a, b"}, false},               // unclosed bracket
		{`a], b`, []string{"a]", " b"}, false},            // stray closer: clamped split, unbalanced
		{`a)(b`, []string{"a)(b"}, false},                 // net-zero but stray closer: unbalanced
	}
	for _, c := range cases {
		got, balanced := Split(c.s, ',')
		if !reflect.DeepEqual(got, c.want) || balanced != c.balanced {
			t.Errorf("Split(%q) = %v, %v; want %v, %v", c.s, got, balanced, c.want, c.balanced)
		}
	}
}

func TestBalanced(t *testing.T) {
	cases := map[string]bool{
		``:           true,
		`'a' => [1]`: true,
		`('x')`:      true,
		`{[()]}`:     true,
		`'unclosed`:  false,
		`"unclosed`:  false,
		`[open`:      false,
		`close]`:     false,
		`a)(b`:       false, // net zero, but via a stray closer
		`'\''`:       true,  // escaped quote then real closer
		`'\\'`:       true,  // escaped backslash then real closer
	}
	for s, want := range cases {
		if got := Balanced(s); got != want {
			t.Errorf("Balanced(%q) = %v; want %v", s, got, want)
		}
	}
}
