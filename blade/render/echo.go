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
// numeric &#039;, matching PHP's default ENT_HTML401), and any ill-formed UTF-8
// is replaced by U+FFFD — the ENT_SUBSTITUTE behavior Laravel relies on so a
// stray byte never blanks the whole string.
//
// The substitution follows PHP's per-subsequence rule (oracle-verified, not the
// Unicode range-aware "maximal subpart"): at an ill-formed offset, consume the
// lead byte plus following continuation bytes (0x80–0xBF) up to the lead's
// modern-UTF-8 length, and emit a SINGLE U+FFFD for the whole run. So a truncated
// three-byte lead like E2 82 collapses to one U+FFFD, not two. Go's
// utf8.DecodeRuneInString returns (RuneError, 1) on any ill-formed offset — that
// flags the substitution; phpSubpartLen then decides how many bytes it swallows.
func Escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune('�') // ill-formed subsequence → one replacement char
			i += phpSubpartLen(s[i:])
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

// phpSubpartLen returns how many bytes an ill-formed UTF-8 subsequence at the
// start of s collapses into one U+FFFD, matching PHP's htmlspecialchars: the
// lead byte plus continuation bytes (0x80–0xBF) up to the length the lead
// declares (0xC2–0xDF → 2, 0xE0–0xEF → 3, 0xF0–0xF4 → 4). Every other lead —
// a stray continuation byte, an overlong 0xC0/0xC1, or an out-of-range
// 0xF5–0xFF — declares length 1 and swallows only itself. s is non-empty and
// known ill-formed at offset 0 (the caller checked DecodeRuneInString).
func phpSubpartLen(s string) int {
	var n int
	switch b := s[0]; {
	case b >= 0xC2 && b <= 0xDF:
		n = 2
	case b >= 0xE0 && b <= 0xEF:
		n = 3
	case b >= 0xF0 && b <= 0xF4:
		n = 4
	default:
		return 1 // continuation byte as lead, or an invalid lead: one byte
	}
	consumed := 1
	for consumed < n && consumed < len(s) && s[consumed] >= 0x80 && s[consumed] <= 0xBF {
		consumed++
	}
	return consumed
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
	case rawHTML:
		// Pre-rendered slot HTML. The echo path intercepts this before Stringify;
		// handled here too so any other path stringifies it without error.
		return string(x), nil
	case []expr.Value, map[string]expr.Value:
		return "", errNotStringable
	default:
		return "", errNotStringable
	}
}
