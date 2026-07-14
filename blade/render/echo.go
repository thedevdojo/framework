package render

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/thedevdojo/pocketknife/blade/expr"
)

// Escape mirrors Laravel's e(): htmlspecialchars($s, ENT_QUOTES|ENT_SUBSTITUTE,
// 'UTF-8'). The five HTML-significant runes become entities (single quote as the
// numeric &#039;, matching PHP's default ENT_HTML401), and any byte that is not
// valid UTF-8 is replaced by U+FFFD — the ENT_SUBSTITUTE behavior Laravel relies
// on so a stray byte never blanks the whole string.
func Escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune('�') // invalid byte → replacement character
			i++
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#039;")
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// errNotStringable is returned by Stringify for values PHP cannot echo as a
// scalar (arrays and maps). The renderer turns it into an expression-error
// diagnostic.
var errNotStringable = errors.New("value is not stringable")

// Stringify renders a value the way PHP's echo would cast it to a string:
//
//	null  → ""     false → ""     true → "1"
//	int   → decimal
//	float → shortest round-trip (Go's 'G' with precision -1)
//	string→ itself
//	array/map → error (PHP would emit "Array" plus a notice — out of scope)
//
// Float formatting matches PHP echo for the common finite decimals templates
// carry (1.0→"1", 2.5→"2.5", 0.1→"0.1"). PHP's echo uses precision=14 and its
// own exponent form ("1.0E+20"), so values needing >14 significant digits or
// scientific notation can diverge; those are documented, not exercised by
// oracle fixtures. See .superpowers/sdd/task-6-report.md.
func Stringify(v expr.Value) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case bool:
		if x {
			return "1", nil
		}
		return "", nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'G', -1, 64), nil
	case string:
		return x, nil
	case []expr.Value, map[string]expr.Value:
		return "", errNotStringable
	default:
		return "", errNotStringable
	}
}
