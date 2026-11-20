package render

import (
	"strconv"
	"strings"

	"github.com/thedevdojo/framework/blade/expr"
	"github.com/thedevdojo/framework/blade/internal/strscan"
)

// parseProps parses a @props(...) argument — a PHP literal array — into declared
// props. It is a small, dedicated grammar (NOT the expression evaluator):
//
//	array   := '[' (entry (',' entry)* ','?)? ']'
//	entry   := string '=>' default  // keyed: prop name + default
//	         | string                // bare: prop name, no default
//	default := scalar
//	         | '[' (scalar (',' scalar)* ','?)? ']'   // empty or flat scalar list
//	scalar  := string | number | 'true' | 'false' | 'null'
//	string  := '...' | "..."   (only \\ and the matching quote are escapes)
//
// A bare entry must be a string (its value becomes the prop name). Anything
// outside this grammar returns ok=false, which the renderer surfaces as a props
// diagnostic and treats as no declared props — EXCEPT a keyed default that is a
// valid-but-unsupported array shape (nested or associative): the prop is then
// declared WITHOUT a default and a precise message is appended to issues, which
// the renderer surfaces as an app-tier diagnostic (real Blade accepts any PHP
// literal there; this engine models only the empty/flat-list shapes).
func parseProps(raw string) (props propSet, issues []string, ok bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, nil, false
	}
	inner := s[1 : len(s)-1]
	entries, ok := splitTopLevel(inner, ',')
	if !ok {
		return nil, nil, false
	}

	out := propSet{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue // tolerate a trailing comma / empty array
		}
		key, val, hasArrow, ok := splitArrowEntry(e)
		if !ok {
			return nil, nil, false
		}
		if hasArrow {
			name, ok := parseStringLiteral(strings.TrimSpace(key))
			if !ok {
				return nil, nil, false
			}
			def, supported, ok := parseDefault(strings.TrimSpace(val))
			if !ok {
				return nil, nil, false
			}
			if !supported {
				issues = append(issues, "@props default for \""+name+"\" is a nested or associative array, outside the DevDojo Framework subset; requires the PHP app tier")
				out[name] = propDecl{hasDefault: false}
				continue
			}
			out[name] = propDecl{hasDefault: true, def: def}
		} else {
			name, ok := parseStringLiteral(e) // bare entry: a string prop name
			if !ok {
				return nil, nil, false
			}
			out[name] = propDecl{hasDefault: false}
		}
	}
	return out, issues, true
}

// parseDefault parses one keyed entry's default value. Scalars parse per
// parseScalar. Array literals are supported in exactly the two shapes the
// value model can carry as a PHP array: the empty array ([]) and a flat list
// of scalars (['a', 'b'] → []expr.Value, iterated/tested like any array;
// oracle-checked by the props-array-defaults fixture). A keyed (associative)
// or nested array default is valid real Blade this engine cannot model:
// supported=false with ok=true, so the caller declares the prop without a
// default and diagnoses precisely instead of silently degrading. ok=false
// means the text is outside the grammar entirely (malformed @props).
func parseDefault(s string) (def expr.Value, supported, ok bool) {
	if s == "" {
		return nil, false, false
	}
	if s[0] != '[' {
		v, ok := parseScalar(s)
		return v, ok, ok
	}
	if s[len(s)-1] != ']' {
		return nil, false, false
	}
	entries, ok := splitTopLevel(s[1:len(s)-1], ',')
	if !ok {
		return nil, false, false
	}
	list := []expr.Value{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue // trailing comma / empty array
		}
		_, _, hasArrow, entryOK := splitArrowEntry(e)
		if !entryOK {
			return nil, false, false
		}
		if hasArrow {
			return nil, false, true // associative: valid PHP, outside the subset
		}
		if e[0] == '[' {
			return nil, false, true // nested array: valid PHP, outside the subset
		}
		v, ok := parseScalar(e)
		if !ok {
			return nil, false, false
		}
		list = append(list, v)
	}
	return list, true, true
}

// splitArrowEntry splits an entry on its first top-level "=>" (strscan's
// shared quote/depth rules). Returns the key/value halves and whether an
// arrow was present. ok=false on a malformed (unbalanced) arrow-less entry —
// strscan.Balanced is a shade stricter than the historical quote-only check
// (a stray closer now also fails), which only tightens inputs the literal
// parsers downstream rejected anyway.
func splitArrowEntry(e string) (key, val string, hasArrow, ok bool) {
	if i := strscan.Index(e, "=>"); i >= 0 {
		return e[:i], e[i+2:], true, true
	}
	if !strscan.Balanced(e) {
		return "", "", false, false
	}
	return e, "", false, true
}

// splitTopLevel splits s on the given separator byte at bracket/paren depth 0
// and outside string literals (strscan's shared rules). ok=false when the
// text is unbalanced — an unterminated quote, an unclosed bracket, or a
// stray closer (the last is stricter than the historical net-depth check,
// which accepted stray-closer input like `a)(b`).
func splitTopLevel(s string, sep byte) ([]string, bool) {
	parts, balanced := strscan.Split(s, sep)
	if !balanced {
		return nil, false
	}
	return parts, true
}

// parseStringLiteral parses a single quoted string literal (the whole token must
// be the literal). Only \\ and the matching quote are recognized escapes — the
// same rule as the expression tokenizer.
func parseStringLiteral(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	q := s[0]
	if (q != '\'' && q != '"') || s[len(s)-1] != q {
		return "", false
	}
	var b strings.Builder
	inner := s[1 : len(s)-1]
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c == '\\' && i+1 < len(inner) {
			if nx := inner[i+1]; nx == q || nx == '\\' {
				b.WriteByte(nx)
				i++
				continue
			}
			b.WriteByte('\\')
			continue
		}
		if c == q {
			return "", false // an unescaped quote inside means this isn't one literal
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

// parseScalar parses a default value: a string literal, an int, a float, or the
// keywords true/false/null.
func parseScalar(s string) (expr.Value, bool) {
	if s == "" {
		return nil, false
	}
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	case "null":
		return nil, true
	}
	if s[0] == '\'' || s[0] == '"' {
		return parseStringLiteral(s)
	}
	// number: optional leading '-', digits, optional single '.' fractional part.
	if isNumericLiteral(s) {
		if strings.ContainsRune(s, '.') {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f, true
			}
			return nil, false
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true // integer overflow → float, matching the expr evaluator
		}
	}
	return nil, false
}

// isNumericLiteral reports whether s is a plain integer or decimal literal
// (optionally signed) — no exponent, no leading dot.
func isNumericLiteral(s string) bool {
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	digits, dots := 0, 0
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}
