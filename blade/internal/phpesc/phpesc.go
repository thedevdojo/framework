// Package phpesc implements Laravel's e() escape — htmlspecialchars with
// ENT_QUOTES|ENT_SUBSTITUTE in UTF-8 — byte-for-byte, including PHP's
// per-subsequence U+FFFD substitution for ill-formed UTF-8. It lives in an
// internal package because both the renderer (echo escaping) and the
// expression evaluator (the $attributes bag's merge() escaping) need it
// without importing each other.
package phpesc

import (
	"strings"
	"unicode/utf8"
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
// modern-UTF-8 length — plus, when that run stops early at a byte that can never
// start a sequence (0xC0/0xC1/0xF5–0xFF), that one byte too — and emit a SINGLE
// U+FFFD for the whole run. So a truncated three-byte lead like E2 82 collapses
// to one U+FFFD, not two, and C3 FF is also just one. Go's
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
// start of s collapses into one U+FFFD, matching PHP's htmlspecialchars
// (oracle-verified byte-for-byte). Two-part rule:
//
//  1. Continuation run: the lead byte plus continuation bytes (0x80–0xBF) up
//     to the length the lead declares (0xC2–0xDF → 2, 0xE0–0xEF → 3,
//     0xF0–0xF4 → 4). Every other lead — a stray continuation byte, an
//     overlong 0xC0/0xC1, or an out-of-range 0xF5–0xFF — declares length 1.
//  2. Trailing-invalid-lead absorption: when the run stops EARLY (before the
//     declared length) at a byte that cannot start any sequence — 0xC0, 0xC1,
//     or 0xF5–0xFF — that ONE byte is absorbed into the same U+FFFD (PHP's
//     `utf8_lead(next) ? k : k+1`). So C3 FF and E2 82 C0 each collapse to a
//     single replacement. A terminator that IS a valid lead (ASCII or
//     0xC2–0xF4) is left for the next iteration (C3 C2 A9 → U+FFFD + "©"),
//     absorption never chains past that one byte (C3 FF FF → two U+FFFD), and
//     it never applies after a FULL-length run (E0 80 80 FF → two U+FFFD).
//
// s is non-empty and known ill-formed at offset 0 (the caller checked
// DecodeRuneInString).
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
	// Early stop at a byte that can never start a sequence → absorb it. The
	// terminator here cannot be 0x80–0xBF (the loop would have consumed it), so
	// the invalid-lead set reduces to 0xC0/0xC1/0xF5–0xFF.
	if consumed < n && consumed < len(s) {
		if b := s[consumed]; b == 0xC0 || b == 0xC1 || b >= 0xF5 {
			consumed++
		}
	}
	return consumed
}
