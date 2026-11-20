package htmlscan

import "strings"

// This file is the scanner's half of `devdojo convert`: the three things
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

// Literal writes text so that Scan, reading it at ctx, reads every byte back
// as plain text. It moves ctx over the text, as Scan would.
//
// Most text needs nothing. What Scan would take for the template's is defused
// the least intrusive way that is exact. Where a brace is read, a brace that
// would open a value, or that follows a backslash, is written \{. Elsewhere
// braces get the @{{ escape. Where no escape exists the first byte is printed
// by a raw echo of a one-character string — {!! '<' !!}Button> prints
// <Button> and is not a tag. An HTML comment that closes inside the text is
// already opaque and is copied as it is; one that does not is defused, so it
// cannot swallow what follows.
//
// The walk mirrors Scan's own dispatch, in the same order.
func Literal(text string, ctx *Context) string {
	var b strings.Builder
	b.Grow(len(text) + 16)
	i, n := 0, len(text)
	// tail counts the '{' and '\' bytes the output ends in that were written
	// as text. Such a byte would fuse with a hatch after it — "{{" opens
	// braces, and where a brace is read "\{" is the escape — so it is hatched
	// too.
	tail := 0
	put := func(s string) {
		b.WriteString(s)
		ctx.Feed(s)
		i += len(s)
		if s == "{" || s == "\\" {
			tail++
		} else {
			tail = 0
		}
	}
	hatch := func() {
		out := b.String()
		cut := len(out)
		for ; tail > 0; tail-- {
			if c := out[cut-1]; c != '{' && !(c == '\\' && ctx.Reads()) {
				break
			}
			cut--
		}
		if cut < len(out) {
			b.Reset()
			b.WriteString(out[:cut])
			for j := cut; j < len(out); j++ {
				b.WriteString(Hatch(out[j]))
			}
		}
		b.WriteString(Hatch(text[i]))
		ctx.Construct()
		i++
		tail = 0
	}
	for i < n {
		rest := text[i:]
		if text[i] == '{' && ctx.Reads() {
			if escapeBrace(text, i) {
				b.WriteString(`\`)
				put("{")
				tail = 0
				continue
			}
			put("{")
			continue
		}
		switch {
		case text[i] == '@' && ctx.Reads():
			// The braces after it are escaped with a backslash, which is
			// not the @{{ escape.
			put("@")
		case strings.HasPrefix(rest, "@{{"):
			// The '@' is real. Before braces that close it can simply be
			// written: they get their own escape below, and "@@{{ x }}" reads
			// as an at-sign and then escaped braces. Before braces left open
			// it would fuse with the hatch they get, so it is hatched too.
			if strings.Contains(rest[3:], "}}") {
				put("@")
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
			b.WriteByte('@')
			put(rest[:stop])
		case ctx.elem == "" && strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				hatch()
				continue
			}
			put(rest[:4+end+3])
		case strings.HasPrefix(rest, "</"):
			class, lower := classify(tagName(rest[2:]))
			if class == tagRawText && lower == ctx.elem {
				ctx.elem = ""
			}
			if class == tagBuiltin || (class == tagComponent && ctx.elem == "") {
				hatch()
				continue
			}
			put("<")
		case text[i] == '<':
			class, lower := classify(tagName(rest[1:]))
			if class == tagRawText && ctx.elem == "" {
				ctx.elem = lower
			}
			if class == tagBuiltin || (class == tagComponent && ctx.elem == "") {
				hatch()
				continue
			}
			put("<")
		default:
			put(text[i : i+1])
		}
	}
	return b.String()
}

// escapeBrace reports whether the brace at text[i], where a brace is read,
// must be written \{ to stay text: it follows a backslash (which would
// otherwise escape it), or it opens something — {{, {!!, or a value.
func escapeBrace(text string, i int) bool {
	rest := text[i:]
	if i > 0 && text[i-1] == '\\' {
		return true
	}
	if strings.HasPrefix(rest, "{{") || strings.HasPrefix(rest, "{!!") {
		return true
	}
	_, ok := singleAt(rest)
	return ok
}

// EscapeBraces writes text that a component attribute's value holds so that
// the value reads back as exactly that text: every brace that would open a
// value, or follows a backslash, becomes \{. It is for an attribute that
// holds no code; there a brace is never read.
func EscapeBraces(text string) string {
	if !strings.Contains(text, "{") {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '{' && escapeBrace(text, i) {
			b.WriteByte('\\')
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// Hatch prints one character through a raw echo, so the scanner reading it
// sees an expression and not the start of whatever that character would have
// begun: {!! '<' !!}Button> prints <Button> and is not a tag.
func Hatch(c byte) string {
	if c == '\\' || c == '\'' {
		return "{!! '\\" + string(c) + "' !!}"
	}
	return "{!! '" + string(c) + "' !!}"
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

// Interpolate reads the value of a component attribute named name the way the
// component tag does: text that embeds {name} or {{ name }} is the expression
// joining its pieces, so "Hi {name}" is 'Hi ' . $name. In an attribute that
// holds code only {{ }} is read. ok is false for a value that embeds nothing;
// then expr is the text the value means. The converter uses it to check that a
// joined expression can be written back as text.
func Interpolate(name, value string) (expr string, ok bool) {
	return interpolate(value, !IsCodeAttr(name))
}
