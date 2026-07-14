package render

import (
	"strconv"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/expr"
)

// parseProps parses a @props(...) argument — a PHP literal array — into declared
// props. It is a small, dedicated grammar (NOT the expression evaluator):
//
//	array   := '[' (entry (',' entry)* ','?)? ']'
//	entry   := string '=>' scalar   // keyed: prop name + default
//	         | string                // bare: prop name, no default
//	scalar  := string | number | 'true' | 'false' | 'null'
//	string  := '...' | "..."   (only \\ and the matching quote are escapes)
//
// A bare entry must be a string (its value becomes the prop name). Anything
// outside this grammar returns ok=false, which the renderer surfaces as a props
// diagnostic and treats as no declared props.
func parseProps(raw string) (propSet, bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, false
	}
	inner := s[1 : len(s)-1]
	entries, ok := splitTopLevel(inner, ',')
	if !ok {
		return nil, false
	}

	out := propSet{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue // tolerate a trailing comma / empty array
		}
		key, val, hasArrow, ok := splitArrowEntry(e)
		if !ok {
			return nil, false
		}
		if hasArrow {
			name, ok := parseStringLiteral(strings.TrimSpace(key))
			if !ok {
				return nil, false
			}
			def, ok := parseScalar(strings.TrimSpace(val))
			if !ok {
				return nil, false
			}
			out[name] = propDecl{hasDefault: true, def: def}
		} else {
			name, ok := parseStringLiteral(e) // bare entry: a string prop name
			if !ok {
				return nil, false
			}
			out[name] = propDecl{hasDefault: false}
		}
	}
	return out, true
}

// splitArrowEntry splits an entry on its first top-level "=>". Returns the
// key/value halves and whether an arrow was present. ok=false only on a
// malformed (unbalanced) entry.
func splitArrowEntry(e string) (key, val string, hasArrow, ok bool) {
	depth := 0
	var quote byte
	for i := 0; i < len(e); i++ {
		c := e[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth == 0 && i+1 < len(e) && e[i+1] == '>' {
				return e[:i], e[i+2:], true, true
			}
		}
	}
	if quote != 0 {
		return "", "", false, false
	}
	return e, "", false, true
}

// splitTopLevel splits s on the given separator byte at bracket/paren depth 0
// and outside string literals. ok=false when a quote is left unterminated.
func splitTopLevel(s string, sep byte) ([]string, bool) {
	var parts []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, false
	}
	parts = append(parts, s[start:])
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
