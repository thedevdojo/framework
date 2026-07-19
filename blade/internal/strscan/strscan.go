// Package strscan is the ONE quote/depth mini-scanner behind every
// "split/find at top level" need in the Blade pipeline. Four near-identical
// scanners used to live in blade/parser (splitLastAs, splitArrow) and
// blade/render (splitArrowEntry, splitTopLevel) with subtly different depth
// handling; they now all defer here, so the rules below are THE rules:
//
//   - Quotes: '\” and '"' open a string; inside one, a backslash skips the
//     next byte, and only the matching quote closes it. Nothing inside a
//     string is top-level.
//   - Depth: '(', '[', '{' increment; ')', ']', '}' decrement, CLAMPED at
//     zero — a stray closer keeps the scan at top level (so scanning stays
//     resilient to malformed input) but marks it unbalanced.
//   - Top level: outside any quote, at depth zero.
//   - Balanced: every quote closed, depth back at zero, no stray closer.
//
// The clamping choice follows the parser's historical scanners; the render
// package's scanners historically let depth go negative and only checked
// quotes, which made "balanced" accept stray-closer input like `a)(b`. The
// shared Balanced is stricter — callers that gate on it reject such input
// outright, which every call site already handled as malformed downstream.
package strscan

import "strings"

// walk scans s under the package's quote/depth rules, calling visit (when
// non-nil) at each top-level byte index; visit returns true to stop the walk
// early. The return value reports balance and is meaningful only for a full
// walk — an early stop returns the balance seen so far.
func walk(s string, visit func(i int) bool) bool {
	depth := 0
	var quote byte
	balanced := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++ // skip the escaped byte
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if depth == 0 && visit != nil && visit(i) {
			return balanced
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				balanced = false // stray closer: clamp, but the scan is unbalanced
			} else {
				depth--
			}
		}
	}
	return balanced && quote == 0 && depth == 0
}

// Index returns the byte index of the first top-level occurrence of tok in s,
// or -1. The match point must be top-level; tok's own bytes are not
// re-interpreted (the tokens used here — "=>", " as " — are state-neutral).
func Index(s, tok string) int {
	found := -1
	walk(s, func(i int) bool {
		if strings.HasPrefix(s[i:], tok) {
			found = i
			return true
		}
		return false
	})
	return found
}

// LastIndex returns the byte index of the last top-level occurrence of tok in
// s, or -1.
func LastIndex(s, tok string) int {
	last := -1
	walk(s, func(i int) bool {
		if strings.HasPrefix(s[i:], tok) {
			last = i
		}
		return false
	})
	return last
}

// Split splits s on the single-byte separator sep at top level. The final
// segment is always appended, so len(parts) is one more than the separator
// count. balanced=false flags an unterminated quote, an unclosed bracket, or
// a stray closer — callers treating s as a delimited literal should reject
// the parts then.
func Split(s string, sep byte) (parts []string, balanced bool) {
	start := 0
	balanced = walk(s, func(i int) bool {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
		return false
	})
	parts = append(parts, s[start:])
	return parts, balanced
}

// Balanced reports whether s scans clean under the package's rules: quotes
// terminated, brackets closed, no stray closer.
func Balanced(s string) bool {
	return walk(s, nil)
}
