package convert

import (
	"strings"

	"github.com/thedevdojo/pocketknife/blade/htmlscan"
	"github.com/thedevdojo/pocketknife/blade/internal/strscan"
	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// This file is the converter's referee. The two scanners emit the same tokens
// for the same template, so "did the conversion keep the meaning" is "do the
// two files scan to the same tokens" — once the handful of things that are
// spelled differently without meaning anything different are levelled:
//
//   - a hatch ({!! '<' !!}) is the text it prints, and a separator ({{ '' }})
//     is nothing at all
//   - @foreach's arguments and the lists inside @props and @vite are compared
//     by what they say, not by their spacing or which quote they use
//   - an HTML component tag is compared by the file it resolves to
//   - <Foreach limit="3"> is @foreach followed by @break($loop->index >= 3)
//   - a directive Laravel does not know (@media) is the text it prints

// item is one token with everything that does not carry meaning removed.
type item struct {
	kind  scanner.Kind
	name  string
	text  string
	attrs []scanner.Attr
	self  bool
	from  int // index of the token this came from, for pointing at a mismatch
}

// mismatch says where two token streams part: the index of the token on each
// side. ok is false when a side simply ran out.
type mismatch struct {
	blade, html int
	ok          bool
}

// tokenAt returns the original token a mismatch points at.
func tokenAt(origin []scanner.Token, at mismatch, originIsBlade bool) (scanner.Token, bool) {
	i := at.html
	if originIsBlade {
		i = at.blade
	}
	if i < 0 || i >= len(origin) {
		return scanner.Token{}, false
	}
	return origin[i], true
}

// equivalent reports whether a Blade token stream and a Studio-syntax one say
// the same thing, and where they first part if not.
//
// foldUnknown says what a directive outside the engine's subset means on the
// Blade side. Going Blade → HTML it is whatever the author wrote, and one
// Laravel does not know is just the text it prints (the caller has already
// refused the ones Laravel does know). Going HTML → Blade the Blade side is
// the converter's own output, where an unknown directive is a piece of text
// that got out unescaped — never equal to anything.
func equivalent(blade, html []scanner.Token, foldUnknown bool, exists func(string) bool) (mismatch, bool) {
	a := normalize(blade, false, foldUnknown, exists)
	b := normalize(html, true, false, exists)
	for i := 0; i < len(a) && i < len(b); i++ {
		if !sameItem(a[i], b[i]) {
			return mismatch{blade: a[i].from, html: b[i].from, ok: true}, false
		}
	}
	switch {
	case len(a) > len(b):
		return mismatch{blade: a[len(b)].from, html: len(html), ok: true}, false
	case len(b) > len(a):
		return mismatch{blade: len(blade), html: b[len(a)].from, ok: true}, false
	}
	return mismatch{}, true
}

func sameItem(a, b item) bool {
	if a.kind == scanner.Error || b.kind == scanner.Error {
		return false
	}
	if a.kind != b.kind || a.name != b.name || a.text != b.text || a.self != b.self || len(a.attrs) != len(b.attrs) {
		return false
	}
	for i := range a.attrs {
		x, y := a.attrs[i], b.attrs[i]
		if x.Name != y.Name || x.Value != y.Value || x.Bound != y.Bound || x.Valueless != y.Valueless {
			return false
		}
	}
	return true
}

func normalize(toks []scanner.Token, html, foldUnknown bool, exists func(string) bool) []item {
	var out []item
	text := func(i int, s string) {
		if s == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].kind == scanner.Text {
			out[n-1].text += s
			return
		}
		out = append(out, item{kind: scanner.Text, text: s, from: i})
	}
	for i, tok := range toks {
		switch tok.Kind {
		case scanner.Text:
			text(i, tok.Text)

		case scanner.RawEcho:
			if c, ok := hatched(tok.Text); ok {
				text(i, string(c))
				continue
			}
			out = append(out, item{kind: tok.Kind, text: tok.Text, from: i})

		case scanner.Echo:
			if tok.Text == "''" {
				continue // a separator prints nothing
			}
			out = append(out, item{kind: tok.Kind, text: tok.Text, from: i})

		case scanner.Comment, scanner.SlotClose, scanner.Error:
			out = append(out, item{kind: tok.Kind, from: i})

		case scanner.SlotOpen:
			out = append(out, item{kind: tok.Kind, name: tok.Name, self: tok.SelfClose, from: i})

		case scanner.TagOpen:
			name := tok.Name
			if html {
				name = resolve(tok.Candidates, exists)
			}
			out = append(out, item{kind: tok.Kind, name: name, attrs: tok.Attrs, self: tok.SelfClose, from: i})

		case scanner.TagClose:
			name := tok.Name
			if html && htmlscan.IsComponentTag(tok.Tag) {
				name = resolve(htmlscan.Candidates(tok.Tag), exists)
			}
			out = append(out, item{kind: tok.Kind, name: name, from: i})

		case scanner.Directive:
			switch tok.Name {
			case "foreach":
				out = append(out, item{kind: tok.Kind, name: tok.Name, text: canonForeach(tok.Text), from: i})
				if limit, ok := loopLimit(tok); ok {
					// The break goes on the line below the loop when there is
					// one — each directive swallows the line break after it,
					// so one extra line break here changes nothing rendered.
					if i+1 < len(toks) && toks[i+1].Kind == scanner.Text && strings.HasPrefix(toks[i+1].Text, "\n") {
						out = append(out, item{kind: scanner.Text, text: "\n", from: i})
					}
					out = append(out, item{kind: tok.Kind, name: "break", text: limitCondition(limit), from: i})
				}
			case "props":
				out = append(out, item{kind: tok.Kind, name: tok.Name, text: canonProps(tok.Text), from: i})
			case "vite":
				out = append(out, item{kind: tok.Kind, name: tok.Name, text: canonVite(tok.Text), from: i})
			case "if", "elseif", "else", "endif", "endforeach", "break", "continue", "class", "style":
				out = append(out, item{kind: tok.Kind, name: tok.Name, text: tok.Text, from: i})
			default:
				if !html && foldUnknown {
					text(i, tok.Src)
					continue
				}
				out = append(out, item{kind: tok.Kind, name: "@" + tok.Name, text: tok.Src, from: i})
			}
		}
	}
	return out
}

// hatched recognizes a hatch's expression — one of the three characters that
// ever need one, as a single-quoted string — and returns the character. Only
// these three: text and a raw echo are the same thing on the page unless a
// line break is involved, which a tag before it would swallow.
func hatched(expr string) (byte, bool) {
	if len(expr) == 3 && expr[0] == '\'' && expr[2] == '\'' && strings.IndexByte("@{<", expr[1]) >= 0 {
		return expr[1], true
	}
	return 0, false
}

// resolve picks the component file a list of candidates means: the first that
// exists, or the first of all when none does (the name a "component not
// found" message carries).
func resolve(cands []string, exists func(string) bool) string {
	if len(cands) == 0 {
		return ""
	}
	for _, c := range cands {
		if exists(c) {
			return dotted(c)
		}
	}
	return dotted(cands[0])
}

// dotted is a component path as Blade names it: sections/hero → sections.hero.
func dotted(base string) string { return strings.ReplaceAll(base, "/", ".") }

// loopLimit returns the limit a Studio-syntax <Foreach limit="…"> carries.
func loopLimit(tok scanner.Token) (string, bool) {
	for _, a := range tok.Attrs {
		if a.Name == "limit" {
			return a.Value, true
		}
	}
	return "", false
}

// limitCondition is the @break condition a loop limit turns into.
func limitCondition(limit string) string {
	if !simpleOperand(limit) {
		limit = "(" + limit + ")"
	}
	return "$loop->index >= " + limit
}

// simpleOperand reports whether a Blade expression is a plain number or a
// variable with member accesses — safe beside an operator without parentheses.
func simpleOperand(e string) bool {
	if e == "" {
		return false
	}
	digits := true
	for i := 0; i < len(e); i++ {
		if e[i] < '0' || e[i] > '9' {
			digits = false
			break
		}
	}
	if digits {
		return true
	}
	if e[0] != '$' {
		return false
	}
	for _, seg := range strings.Split(e[1:], "->") {
		if !isName(seg) {
			return false
		}
	}
	return true
}

// loopParts splits @foreach's arguments — "$posts as $key => $post" — the way
// the parser does: on the last top-level " as ", then on "=>".
func loopParts(text string) (items, key, val string, ok bool) {
	i := strscan.LastIndex(text, " as ")
	if i < 0 {
		return "", "", "", false
	}
	items = strings.TrimSpace(text[:i])
	right := text[i+4:]
	if j := strscan.Index(right, "=>"); j >= 0 {
		// An arrow with nothing before it ("as => $post") is not the no-key
		// form: the parser rejects that loop, so it is not a loop here either.
		key = strings.TrimSpace(right[:j])
		if key == "" {
			return "", "", "", false
		}
		return items, key, strings.TrimSpace(right[j+2:]), true
	}
	return items, "", strings.TrimSpace(right), true
}

func canonForeach(text string) string {
	items, key, val, ok := loopParts(text)
	if !ok {
		return strings.TrimSpace(text)
	}
	if key != "" {
		return items + " as " + key + " => " + val
	}
	return items + " as " + val
}

// prop is one entry of a @props list. A bare entry ('title') has no default.
type prop struct {
	name       string
	value      string // the default as written, trimmed; "" when bare
	hasDefault bool
}

// propList reads a @props argument — ['title' => 'Untitled', 'badge'] — into
// its entries. ok is false for anything that is not a list of that shape.
func propList(raw string) ([]prop, bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, false
	}
	entries, balanced := strscan.Split(s[1:len(s)-1], ',')
	if !balanced {
		return nil, false
	}
	var out []prop
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue // a trailing comma
		}
		if i := strscan.Index(e, "=>"); i >= 0 {
			name, ok := stringLiteral(strings.TrimSpace(e[:i]))
			if !ok {
				return nil, false
			}
			out = append(out, prop{name: name, value: strings.TrimSpace(e[i+2:]), hasDefault: true})
			continue
		}
		name, ok := stringLiteral(e)
		if !ok {
			return nil, false
		}
		out = append(out, prop{name: name})
	}
	return out, true
}

// canonProps levels a @props list: one spelling per entry, whatever the
// spacing, line breaks and quotes were.
//
// A bare entry is levelled with "=> null". They are not the same thing in
// Blade — bare means required — but the Studio syntax has only the optional
// kind (<Props badge /> is 'badge' => null), so a required prop becomes an
// optional one on its way to HTML. Nothing rendered changes: a prop that was
// passed is still passed, and a required prop that was not passed was an
// error that printed nothing.
func canonProps(raw string) string {
	props, ok := propList(raw)
	if !ok {
		return strings.TrimSpace(raw)
	}
	parts := make([]string, len(props))
	for i, p := range props {
		value := "null"
		if p.hasDefault {
			value = p.value
			if s, ok := plainString(value); ok {
				value = phpString(s)
			}
		}
		parts[i] = phpString(p.name) + " => " + value
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// viteEntries reads a @vite argument: nothing, one string, or a list of them.
func viteEntries(raw string) ([]string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, true
	}
	if one, ok := stringLiteral(s); ok {
		return []string{one}, true
	}
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, false
	}
	parts, balanced := strscan.Split(s[1:len(s)-1], ',')
	if !balanced {
		return nil, false
	}
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		entry, ok := stringLiteral(part)
		if !ok {
			return nil, false
		}
		out = append(out, entry)
	}
	return out, true
}

func canonVite(raw string) string {
	entries, ok := viteEntries(raw)
	if !ok {
		return strings.TrimSpace(raw)
	}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = phpString(e)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// stringLiteral reads s as one quoted string and returns what it holds, by the
// engine's own rule: a backslash escapes only itself and the quote.
func stringLiteral(s string) (string, bool) {
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
			return "", false
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

// plainString reads s as a string literal that means the same text to this
// engine and to PHP. A single-quoted string always does. A double-quoted one
// does unless PHP would expand something in it — a '$', or a backslash before
// anything but a quote or another backslash (\n, \t). Such a default is not
// text to be respelled; it is kept exactly as written.
func plainString(s string) (string, bool) {
	text, ok := stringLiteral(s)
	if !ok {
		return "", false
	}
	if s[0] == '"' {
		inner := s[1 : len(s)-1]
		for i := 0; i < len(inner); i++ {
			switch inner[i] {
			case '$':
				return "", false
			case '\\':
				if i+1 >= len(inner) || (inner[i+1] != '"' && inner[i+1] != '\\') {
					return "", false
				}
				i++
			}
		}
	}
	return text, true
}
