package convert

import (
	"strings"

	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// A component tag is rewritten in place: its name changes, a bound value is
// respelled, and everything else — which attributes sit on which line, the
// quotes, the spacing before "/>" — is carried over as the author wrote it.
// For that the converter reads the tag's attributes a second time, keeping the
// raw text the scanners throw away.

// rawAttr is one attribute exactly as written.
type rawAttr struct {
	lead  string // the whitespace before it
	name  string // with its ':' or '::', if any
	eq    bool   // it has a value
	quote byte   // '"', '\'' or 0 for an unquoted value
	value string // between the quotes
}

// rawTag is the attribute part of a tag exactly as written.
type rawTag struct {
	attrs     []rawAttr
	tail      string // the whitespace before the closing ">" or "/>"
	selfClose bool
	text      string // the whole tag, from its '<'
}

// lexTag reads the attributes of the tag that starts at src[start], from
// src[at] (just past its name). It follows the scanners' own attribute lexing
// step for step, so wherever they read a tag this reads the same one.
func lexTag(src []byte, start, at int) (rawTag, bool) {
	var t rawTag
	i := at
	for {
		ws := i
		for i < len(src) && isSpace(src[i]) {
			i++
		}
		lead := string(src[ws:i])
		if i >= len(src) {
			return t, false
		}
		switch src[i] {
		case '>':
			t.tail, t.text = lead, string(src[start:i+1])
			return t, true
		case '/':
			if i+1 < len(src) && src[i+1] == '>' {
				t.tail, t.selfClose, t.text = lead, true, string(src[start:i+2])
				return t, true
			}
			return t, false
		}
		ns := i
		for i < len(src) && isAttrNameChar(src[i]) {
			i++
		}
		if i == ns {
			return t, false
		}
		a := rawAttr{lead: lead, name: string(src[ns:i])}
		if i < len(src) && src[i] == '=' {
			a.eq = true
			i++
			if i >= len(src) {
				return t, false
			}
			if q := src[i]; q == '"' || q == '\'' {
				i++
				vs := i
				for i < len(src) && src[i] != q {
					i++
				}
				if i >= len(src) {
					return t, false
				}
				a.quote, a.value = q, string(src[vs:i])
				i++
			} else {
				vs := i
				for i < len(src) && !isSpace(src[i]) && src[i] != '>' && src[i] != '/' {
					i++
				}
				a.value = string(src[vs:i])
			}
		}
		t.attrs = append(t.attrs, a)
	}
}

// isAttrNameChar matches attribute-name bytes, as both scanners do.
func isAttrNameChar(b byte) bool {
	return isIdentPart(b) || b == ':' || b == '-' || b == '.' || b == '@'
}

// isTagNameChar matches tag-name bytes, as both scanners do.
func isTagNameChar(b byte) bool {
	return isIdentPart(b) || b == '.' || b == '-'
}

// tagSource reads the tag a TagOpen token came from. prefix is what stands
// between the '<' and the name ("x-" in Blade, nothing in the HTML syntax).
func (o offsets) tagSource(pos scanner.Pos, prefix string) (rawTag, bool) {
	start := o.at(pos)
	at := start + 1 + len(prefix)
	if at > len(o.src) {
		return rawTag{}, false
	}
	for at < len(o.src) && isTagNameChar(o.src[at]) {
		at++
	}
	return lexTag(o.src, start, at)
}

// static writes a plain or valueless attribute back exactly as it was.
func (a rawAttr) static() string {
	if !a.eq {
		return a.name
	}
	if a.quote == 0 {
		return a.name + "=" + a.value
	}
	return a.name + "=" + string(a.quote) + a.value + string(a.quote)
}

// plainAttr writes a token's attribute with no source to copy from: the
// fallback for a tag the raw lexer could not line up with its token.
func plainAttr(a scanner.Attr) (string, bool) {
	name := a.Name
	if !a.Bound && strings.HasPrefix(name, ":") {
		name = ":" + name // a literal :name is written ::name
	}
	switch {
	case a.Valueless:
		return name, true
	case a.Bound:
		name = ":" + name
	}
	q, ok := pickQuote(a.Value, '"')
	if !ok {
		return "", false
	}
	return name + "=" + string(q) + a.Value + string(q), true
}
