package htmlscan

import "strings"

// This file is the scanner's half of `pocketknife convert`: the three things
// only the scanner can know about its own syntax — how to spell a Blade
// expression the friendly way, how to write text so it scans back as text, and
// whether a name is a component tag.

// Friendly rewrites a Blade expression the way an .html file writes it —
// $post->title becomes post.title, && becomes and — and is the inverse of Expr:
// Expr(Friendly(e)) == e, always. When the friendly spelling would not read
// back exactly (a variable named $and, a spaced ->), the expression is returned
// in Blade form, which an .html file accepts as it is.
func Friendly(blade string) string {
	var b strings.Builder
	b.Grow(len(blade))
	i, n := 0, len(blade)
	for i < n {
		c := blade[i]
		switch {
		case c == '\'' || c == '"':
			j := stringEnd(blade, i)
			b.WriteString(blade[i:j])
			i = j
		case c == '$' && i+1 < n && isIdentStart(blade[i+1]):
			i++ // the sigil goes; the name follows as it is
		case c == '-' && i+2 < n && blade[i+1] == '>' && i > 0 && endsOperand(blade[i-1]) && isIdentStart(blade[i+2]):
			b.WriteByte('.')
			i += 2
		case (c == '&' || c == '|') && i+2 < n && blade[i+1] == c && i > 0 && blade[i-1] == ' ' && blade[i+2] == ' ':
			if c == '&' {
				b.WriteString("and")
			} else {
				b.WriteString("or")
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	if out := b.String(); Expr(out) == blade {
		return out
	}
	return blade
}

// IsComponentTag reports whether Scan reads <name> as a component tag: it is
// capitalized, is not a built-in or shouted HTML, and names at least one file.
func IsComponentTag(name string) bool {
	class, _ := classify(name)
	return class == tagComponent && len(Candidates(name)) > 0
}

// Literal writes text so that Scan, entering it inside the raw-text element raw
// ("script", "style" or ""), reads every byte back as plain text. It returns
// the rewritten text and the raw-text element the scanner is inside at its end.
//
// Most text needs nothing. What Scan would take for the template's is defused
// the least intrusive way that is exact: braces get the @{{ escape, and where
// no escape exists the first byte is printed by a raw echo of a one-character
// string — {!! '<' !!}Button> prints <Button> and is not a tag. An HTML
// comment that closes inside the text is already opaque and is copied as it
// is; one that does not is defused, so it cannot swallow what follows.
//
// The walk mirrors Scan's own dispatch, in the same order.
func Literal(text, raw string) (out, rawAfter string) {
	var b strings.Builder
	b.Grow(len(text) + 16)
	i, n := 0, len(text)
	hatch := func() {
		b.WriteString("{!! '" + text[i:i+1] + "' !!}")
		i++
	}
	for i < n {
		rest := text[i:]
		switch {
		case strings.HasPrefix(rest, "@{{"):
			// The '@' is real. Before braces that close it can simply be
			// written: they get their own escape below, and "@@{{ x }}" reads
			// as an at-sign and then escaped braces. Before braces left open
			// it would fuse with the hatch they get, so it is hatched too.
			if strings.Contains(rest[3:], "}}") {
				b.WriteByte('@')
				i++
				continue
			}
			hatch()
		case strings.HasPrefix(rest, "{{--"), strings.HasPrefix(rest, "{!!"):
			hatch()
		case strings.HasPrefix(rest, "{{"):
			end := strings.Index(rest[2:], "}}")
			if end < 0 {
				hatch()
				continue
			}
			stop := 2 + end + 2
			b.WriteString("@" + rest[:stop])
			i += stop
		case raw == "" && strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				hatch()
				continue
			}
			stop := 4 + end + 3
			b.WriteString(rest[:stop])
			i += stop
		case strings.HasPrefix(rest, "</"):
			class, lower := classify(tagName(rest[2:]))
			if class == tagRawText && lower == raw {
				raw = ""
			}
			if class == tagBuiltin || (class == tagComponent && raw == "") {
				hatch()
				continue
			}
			b.WriteByte('<')
			i++
		case text[i] == '<':
			class, lower := classify(tagName(rest[1:]))
			if class == tagRawText && raw == "" {
				raw = lower
			}
			if class == tagBuiltin || (class == tagComponent && raw == "") {
				hatch()
				continue
			}
			b.WriteByte('<')
			i++
		default:
			b.WriteByte(text[i])
			i++
		}
	}
	return b.String(), raw
}

// tagName returns the run of tag-name bytes s starts with.
func tagName(s string) string {
	i := 0
	for i < len(s) && isNameChar(s[i]) {
		i++
	}
	return s[:i]
}

// classCall recognizes {{ class(…) }} and {{ style(…) }}: the whole echo must
// be that one call. It returns the directive's name and its argument text.
func classCall(inner string) (name, args string, ok bool) {
	for _, name := range []string{"class", "style"} {
		if !strings.HasPrefix(inner, name+"(") || !strings.HasSuffix(inner, ")") {
			continue
		}
		args := inner[len(name)+1 : len(inner)-1]
		// The last ')' must be the one that closes the first '(' — otherwise
		// this is some longer expression that merely starts with a call.
		depth := 0
		for i := 0; i < len(args); i++ {
			switch c := args[i]; {
			case c == '\'' || c == '"':
				i = stringEnd(args, i) - 1
			case c == '(':
				depth++
			case c == ')':
				if depth == 0 {
					return "", "", false
				}
				depth--
			}
		}
		if depth != 0 {
			return "", "", false
		}
		return name, strings.TrimSpace(args), true
	}
	return "", "", false
}

// Interpolate reads an attribute value the way a component tag does: text that
// embeds {{ … }} is the expression joining its pieces, so "Hi {{ name }}" is
// 'Hi ' . $name. ok is false for a value that embeds nothing. The converter
// uses it to check that a joined expression can be written back as text.
func Interpolate(value string) (expr string, ok bool) { return interpolate(value) }
