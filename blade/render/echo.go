package render

import (
	"errors"
	"strconv"

	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/blade/internal/phpesc"
)

// Escape mirrors Laravel's e(): htmlspecialchars($s, ENT_QUOTES|ENT_SUBSTITUTE,
// 'UTF-8'). The implementation lives in blade/internal/phpesc (shared with the
// expression evaluator, whose $attributes bag methods escape with e() too);
// this alias keeps the renderer's exported API unchanged.
func Escape(s string) string { return phpesc.Escape(s) }

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
	case []expr.Value, map[string]expr.Value, *expr.Object:
		// PHP can't echo an array, and an object without __toString fatals
		// ("Object of class stdClass could not be converted to string").
		return "", errNotStringable
	default:
		return "", errNotStringable
	}
}
