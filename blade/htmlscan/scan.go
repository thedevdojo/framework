// Package htmlscan is the scanner for the DevDojo Studio syntax — DevDojo Framework's
// second spelling of the template language, used by .html files.
//
// The Studio syntax is Blade written as tags. A capitalized tag is either one of
// the built-in tags (<If>, <Foreach>, <Layout>, <Include>, <Slot>, <Props>, …)
// or a component (<Button>, <SectionsHero>); expressions drop the PHP sigils
// (post.title instead of $post->title). Nothing else changes: this package
// emits the SAME token stream blade/scanner emits for the equivalent Blade
// source, so the parser, the renderer and the expression evaluator are shared
// and the two syntaxes cannot drift apart. TestScanMatchesBlade pins that
// equivalence construct by construct.
//
// What is the template's in an .html file:
//
//   - {{ expr }}, {!! expr !!}, {{-- comment --}}, and the @{{ escape
//   - a tag whose name starts with a capital letter (a real HTML element
//     shouted in ALL CAPS, <BR> or <TABLE>, is still HTML)
//   - <layout> and <include> in lowercase, the DevDojo Static spellings
//
// Everything else is opaque text, emitted verbatim: plain HTML, an Alpine
// @click, a CSS @media, an email address. There are no @directives. An HTML
// comment is opaque too, so commenting a block out really switches it off.
// Inside <script> and <style> a capitalized tag is treated as code rather than
// a component (a<B>c), though the built-in tags and the brace forms still work.
//
// Like the Blade scanner it never panics and never returns an error: a tag it
// cannot read degrades to text, and a tag it can read but that is missing
// something it needs becomes a scanner.Error token carrying the message.
// Positions are 1-based line and column in bytes. Only the Go standard library
// is imported.
package htmlscan

import (
	"bytes"
	"strings"

	"github.com/thedevdojo/framework/blade/scanner"
)

// Scan tokenizes src in a single forward pass.
func Scan(file string, src []byte) []scanner.Token {
	s := &state{file: file, src: src, line: 1, col: 1, open: map[string][]string{}}
	for !s.eof() {
		switch {
		case s.has("@{{"):
			s.escapeBraces()
		case s.has("{{--"):
			s.comment()
		case s.has("{!!"):
			s.echo("{!!", "!!}", scanner.RawEcho)
		case s.has("{{"):
			s.echo("{{", "}}", scanner.Echo)
		case s.raw == "" && s.has("<!--"):
			s.htmlComment()
		case s.has("</"):
			s.closeTag()
		case s.src[s.pos] == '<':
			s.openTag()
		default:
			s.text()
		}
	}
	return s.tokens
}

type state struct {
	file   string
	src    []byte
	pos    int // byte offset of the cursor
	line   int
	col    int
	tokens []scanner.Token
	// raw is "script" or "style" while the cursor is inside that element, where
	// a capitalized tag is code rather than a component.
	raw string
	// open tracks, per paired built-in tag, what each still-open tag resolved
	// to, innermost last. A closing tag pops its entry: a <Layout> learns the
	// component name its opener resolved from src, and "" marks an opener that
	// was refused with an Error token, whose closer is then swallowed so the
	// mistake is reported once.
	open map[string][]string
}

func (s *state) eof() bool { return s.pos >= len(s.src) }

func (s *state) has(prefix string) bool {
	return bytes.HasPrefix(s.src[s.pos:], []byte(prefix))
}

func (s *state) curPos() scanner.Pos {
	return scanner.Pos{File: s.file, Line: s.line, Col: s.col}
}

// adv consumes one byte, advancing the line/column cursor.
func (s *state) adv() {
	if s.eof() {
		return
	}
	b := s.src[s.pos]
	s.pos++
	if b == '\n' {
		s.line++
		s.col = 1
	} else {
		s.col++
	}
}

func (s *state) advanceTo(target int) {
	for s.pos < target && !s.eof() {
		s.adv()
	}
}

func (s *state) save() (int, int, int) { return s.pos, s.line, s.col }

func (s *state) restore(p, l, c int) { s.pos, s.line, s.col = p, l, c }

func (s *state) readWhile(pred func(byte) bool) string {
	start := s.pos
	for !s.eof() && pred(s.src[s.pos]) {
		s.adv()
	}
	return string(s.src[start:s.pos])
}

func (s *state) skipSpaces() {
	for !s.eof() && isSpace(s.src[s.pos]) {
		s.adv()
	}
}

func (s *state) emit(tok scanner.Token) { s.tokens = append(s.tokens, tok) }

// emitText appends text at pos, coalescing with a preceding Text token.
func (s *state) emitText(pos scanner.Pos, text string) {
	if text == "" {
		return
	}
	if n := len(s.tokens); n > 0 && s.tokens[n-1].Kind == scanner.Text {
		s.tokens[n-1].Text += text
		return
	}
	s.emit(scanner.Token{Kind: scanner.Text, Text: text, Pos: pos})
}

// emitDelim emits the next n bytes as literal Text and advances past them.
func (s *state) emitDelim(pos scanner.Pos, n int) {
	start := s.pos
	s.advanceTo(s.pos + n)
	s.emitText(pos, string(s.src[start:s.pos]))
}

// literalLt rewinds to a saved cursor and emits the single '<' as Text, so a
// '<' that turned out not to open a template tag costs nothing but itself.
func (s *state) literalLt(pos scanner.Pos, p, l, c int) {
	s.restore(p, l, c)
	s.adv()
	s.emitText(pos, "<")
}

func (s *state) errorTok(pos scanner.Pos, tag, msg string) {
	s.emit(scanner.Token{Kind: scanner.Error, Name: "<" + tag + ">", Text: msg, Tag: tag, Pos: pos})
}

func (s *state) push(key, val string) { s.open[key] = append(s.open[key], val) }

// pop removes and returns the innermost open entry for key; ok is false when
// no such tag is open (a stray closer).
func (s *state) pop(key string) (val string, ok bool) {
	st := s.open[key]
	if len(st) == 0 {
		return "", false
	}
	s.open[key] = st[:len(st)-1]
	return st[len(st)-1], true
}

// text consumes a run of opaque bytes: the current byte unconditionally, then
// up to (but not including) the next byte that could start a construct.
func (s *state) text() {
	pos := s.curPos()
	start := s.pos
	s.adv()
	for !s.eof() {
		switch s.src[s.pos] {
		case '{', '<', '@':
			s.emitText(pos, string(s.src[start:s.pos]))
			return
		}
		s.adv()
	}
	s.emitText(pos, string(s.src[start:]))
}

// escapeBraces handles "@{{": the '@' is dropped and the brace construct
// through its closing "}}" is literal Text — how a page shows braces to a
// client-side framework. Unterminated: the '@' is literal.
func (s *state) escapeBraces() {
	pos := s.curPos()
	end := bytes.Index(s.src[s.pos+3:], []byte("}}"))
	if end < 0 {
		s.emitDelim(pos, 1)
		return
	}
	stop := s.pos + 3 + end + 2
	text := string(s.src[s.pos+1 : stop])
	s.advanceTo(stop)
	s.emitText(pos, text)
}

// comment handles "{{-- … --}}". Unterminated: the opener is literal Text.
func (s *state) comment() {
	pos := s.curPos()
	end := bytes.Index(s.src[s.pos+4:], []byte("--}}"))
	if end < 0 {
		s.emitDelim(pos, 4)
		return
	}
	s.advanceTo(s.pos + 4 + end + 4)
	s.emit(scanner.Token{Kind: scanner.Comment, Pos: pos})
}

// echo handles {{ … }} and {!! … !!}, rewriting the friendly expression into
// Blade form. Unterminated: the opener is literal Text.
func (s *state) echo(open, close string, kind scanner.Kind) {
	pos := s.curPos()
	rel := bytes.Index(s.src[s.pos+len(open):], []byte(close))
	if rel < 0 {
		s.emitDelim(pos, len(open))
		return
	}
	innerStart := s.pos + len(open)
	inner := strings.TrimSpace(string(s.src[innerStart : innerStart+rel]))
	s.advanceTo(innerStart + rel + len(close))
	if kind == scanner.Echo {
		// {{ class([...]) }} and {{ style([...]) }} are Blade's @class and
		// @style: they sit inside a plain HTML tag, where a tag cannot go.
		if name, args, ok := classCall(inner); ok {
			s.emit(scanner.Token{Kind: scanner.Directive, Name: name, Text: Expr(args), Pos: pos})
			return
		}
	}
	s.emit(scanner.Token{Kind: kind, Text: Expr(inner), Pos: pos})
}

// htmlComment emits <!-- … --> verbatim as opaque Text: nothing inside an HTML
// comment is the template's. Unterminated: the opener is literal Text.
func (s *state) htmlComment() {
	pos := s.curPos()
	end := bytes.Index(s.src[s.pos+4:], []byte("-->"))
	if end < 0 {
		s.emitDelim(pos, 4)
		return
	}
	s.emitDelim(pos, 4+end+3)
}

// tagClass says what a tag name is to the template.
type tagClass int

const (
	tagHTML      tagClass = iota // plain HTML: literal text
	tagRawText                   // <script> / <style>
	tagBuiltin                   // <If>, <Foreach>, <Layout>, …
	tagComponent                 // <Button>, <SectionsHero>
)

// builtins is the set of built-in tag names, lowercased. A built-in is matched
// case-insensitively once the tag starts with a capital (<ForEach>, <Foreach>
// and <FOREACH> are one tag), so none of these can name a component.
var builtins = map[string]bool{
	"if": true, "elseif": true, "else": true,
	"foreach": true, "break": true, "continue": true,
	"layout": true, "include": true, "slot": true,
	"props": true, "vite": true,
}

// classify decides what a tag name is, and returns its lowercase form.
func classify(name string) (tagClass, string) {
	if name == "" || !(isUpper(name[0]) || isLower(name[0])) {
		return tagHTML, ""
	}
	lower := strings.ToLower(name)
	switch {
	case lower == "script" || lower == "style":
		return tagRawText, lower
	case name == "layout" || name == "include":
		return tagBuiltin, lower // the lowercase DevDojo Static spellings
	case !isUpper(name[0]):
		return tagHTML, lower
	case builtins[lower]:
		return tagBuiltin, lower
	case htmlElements[lower] && name == strings.ToUpper(name):
		return tagHTML, lower // shouted HTML: <BR>, <DIV>, <TABLE>
	}
	return tagComponent, lower
}

// htmlElements is every HTML element name, current and obsolete, lowercased.
// It has one job: telling HTML written in capitals (<BR>, <TABLE>, as old
// markup and pasted email templates are) from a component tag. Only an
// ALL-CAPS spelling of one of these is HTML — <Table> and <Button>, written
// the way a component is, are still components.
var htmlElements = func() map[string]bool {
	names := `a abbr acronym address applet area article aside audio b base basefont
		bdi bdo big blink blockquote body br button canvas caption center cite code
		col colgroup data datalist dd del details dfn dialog dir div dl dt em embed
		fieldset figcaption figure font footer form frame frameset h1 h2 h3 h4 h5 h6
		head header hgroup hr html i iframe img input ins kbd label legend li link
		main map mark marquee math menu meta meter nav nobr noframes noscript object
		ol optgroup option output p param picture pre progress q rp rt ruby s samp
		search section select small source span strike strong sub summary sup svg
		table tbody td template textarea tfoot th thead time title tr track tt u ul
		var video wbr`
	set := map[string]bool{}
	for _, name := range strings.Fields(names) {
		set[name] = true
	}
	return set
}()

// openTag handles '<': a built-in tag, a component tag, or — everything else —
// a literal '<'.
func (s *state) openTag() {
	pos := s.curPos()
	p, l, c := s.save()
	s.adv() // '<'
	name := s.readWhile(isNameChar)
	class, lower := classify(name)
	switch class {
	case tagRawText:
		if s.raw == "" {
			s.raw = lower
		}
		s.literalLt(pos, p, l, c)
		return
	case tagHTML:
		s.literalLt(pos, p, l, c)
		return
	case tagComponent:
		if s.raw != "" {
			s.literalLt(pos, p, l, c)
			return
		}
	}
	attrs, selfClose, ok := s.lexAttrs()
	if !ok {
		// Not a tag after all (a<B && c, or a half-typed tag): only the '<' is
		// literal, so whatever follows still scans.
		s.literalLt(pos, p, l, c)
		return
	}
	if class == tagBuiltin {
		s.openBuiltin(pos, name, lower, attrs, selfClose)
		return
	}
	cands := Candidates(name)
	if len(cands) == 0 {
		s.literalLt(pos, p, l, c)
		return
	}
	s.emit(scanner.Token{
		Kind: scanner.TagOpen, Name: dotted(cands[0]), Tag: name, Candidates: cands,
		Attrs: componentAttrs(attrs), SelfClose: selfClose, Pos: pos,
	})
}

// closeTag handles "</": the closer of a built-in or component tag, or a
// literal '<'.
func (s *state) closeTag() {
	pos := s.curPos()
	p, l, c := s.save()
	s.adv() // '<'
	s.adv() // '/'
	name := s.readWhile(isNameChar)
	class, lower := classify(name)
	if class == tagRawText && lower == s.raw {
		s.raw = ""
	}
	if class == tagHTML || class == tagRawText || (class == tagComponent && s.raw != "") {
		s.literalLt(pos, p, l, c)
		return
	}
	s.skipSpaces()
	if s.eof() || s.src[s.pos] != '>' {
		s.literalLt(pos, p, l, c)
		return
	}
	s.adv() // '>'
	if class == tagBuiltin {
		s.closeBuiltin(pos, name, lower)
		return
	}
	cands := Candidates(name)
	if len(cands) == 0 {
		s.literalLt(pos, p, l, c)
		return
	}
	s.emit(scanner.Token{Kind: scanner.TagClose, Name: dotted(cands[0]), Tag: name, Pos: pos})
}

// lexAttrs reads a tag's attributes until '>' or '/>'. It returns the
// attributes, whether the tag self-closes, and false when the bytes do not
// form a tag (EOF, or a byte that cannot start an attribute).
func (s *state) lexAttrs() (attrs []scanner.Attr, selfClose, ok bool) {
	for {
		s.skipSpaces()
		if s.eof() {
			return nil, false, false
		}
		switch s.src[s.pos] {
		case '>':
			s.adv()
			return attrs, false, true
		case '/':
			if s.pos+1 < len(s.src) && s.src[s.pos+1] == '>' {
				s.adv()
				s.adv()
				return attrs, true, true
			}
			return nil, false, false
		}
		aPos := s.curPos()
		raw := s.readWhile(isAttrNameChar)
		if raw == "" {
			return nil, false, false
		}
		bound := false
		name := raw
		switch {
		case strings.HasPrefix(raw, "::"):
			// ::class is the escape for a literal :class attribute (an Alpine or
			// Vue binding), exactly as in Blade: one colon is dropped and the
			// attribute is static.
			name = raw[1:]
		case strings.HasPrefix(raw, ":"):
			bound = true
			name = raw[1:]
		}
		value := ""
		valueless := true
		if !s.eof() && s.src[s.pos] == '=' {
			valueless = false
			s.adv() // '='
			if s.eof() {
				return nil, false, false
			}
			q := s.src[s.pos]
			if q == '"' || q == '\'' {
				s.adv()
				vStart := s.pos
				for !s.eof() && s.src[s.pos] != q {
					s.adv()
				}
				if s.eof() {
					return nil, false, false
				}
				value = string(s.src[vStart:s.pos])
				s.adv()
			} else {
				vStart := s.pos
				for !s.eof() && !isSpace(s.src[s.pos]) && s.src[s.pos] != '>' && s.src[s.pos] != '/' {
					s.adv()
				}
				value = string(s.src[vStart:s.pos])
			}
		}
		attrs = append(attrs, scanner.Attr{Name: name, Value: value, Bound: bound, Valueless: valueless, Pos: aPos})
	}
}

// componentAttrs prepares a component tag's attributes for the renderer: a
// bound attribute's expression is rewritten to Blade form, and a text value
// that embeds {{ }} becomes the bound attribute it is sugar for.
func componentAttrs(attrs []scanner.Attr) []scanner.Attr {
	for i := range attrs {
		a := &attrs[i]
		switch {
		case a.Bound:
			a.Value = Expr(a.Value)
		case !a.Valueless:
			if joined, ok := interpolate(a.Value); ok {
				a.Value, a.Bound = joined, true
			}
		}
	}
	return attrs
}

// interpolate turns a text attribute value that embeds {{ … }} into the
// expression that joins its pieces, and reports whether it embeds any:
//
//	Hi {{ name }}!   →   'Hi ' . $name . '!'
//
// title="Hi {{ name }}" is therefore exactly :title="'Hi ' . name" — one
// meaning, two spellings, and the form Blade can say too. The result is always
// text: a value standing alone is joined onto an empty string so it is not
// passed as a number or a list. {!! … !!} reads the same as {{ … }} here, since an
// attribute's value is escaped when it is printed, never when it is passed. An
// unterminated opener is left as written.
func interpolate(value string) (string, bool) {
	if !strings.Contains(value, "{{") && !strings.Contains(value, "{!!") {
		return value, false
	}
	var parts []string
	exprs := 0
	rest := value
	for {
		open, close := "{{", "}}"
		i := strings.Index(rest, "{{")
		if j := strings.Index(rest, "{!!"); j >= 0 && (i < 0 || j < i) {
			i, open, close = j, "{!!", "!!}"
		}
		if i < 0 {
			break
		}
		end := strings.Index(rest[i+len(open):], close)
		if end < 0 {
			break
		}
		if i > 0 {
			parts = append(parts, phpString(rest[:i]))
		}
		inner := Expr(strings.TrimSpace(rest[i+len(open) : i+len(open)+end]))
		if !isAtom(inner) {
			inner = "(" + inner + ")"
		}
		parts = append(parts, inner)
		exprs++
		rest = rest[i+len(open)+end+len(close):]
	}
	if exprs == 0 {
		return value, false
	}
	if rest != "" {
		parts = append(parts, phpString(rest))
	}
	if len(parts) == 1 {
		parts = []string{"''", parts[0]}
	}
	return strings.Join(parts, " . "), true
}

// isAtom reports whether a Blade-form expression is one operand — a variable
// with its member and index accesses, a number, or a string — and so can sit
// beside the join operator without parentheses.
func isAtom(e string) bool {
	if e == "" {
		return false
	}
	if e[0] == '\'' || e[0] == '"' {
		return stringEnd(e, 0) == len(e)
	}
	if isNumber(e) {
		return e[0] != '-'
	}
	i := 0
	if e[i] == '$' {
		i++
	}
	if i >= len(e) || !isIdentStart(e[i]) {
		return false
	}
	for i < len(e) && isIdentPart(e[i]) {
		i++
	}
	for i < len(e) {
		switch {
		case strings.HasPrefix(e[i:], "->") && i+2 < len(e) && isIdentStart(e[i+2]):
			i += 2
			for i < len(e) && isIdentPart(e[i]) {
				i++
			}
		case e[i] == '[':
			depth := 0
			for ; i < len(e); i++ {
				switch c := e[i]; {
				case c == '\'' || c == '"':
					i = stringEnd(e, i) - 1
				case c == '[':
					depth++
				case c == ']':
					depth--
				}
				if depth == 0 {
					break
				}
			}
			if depth != 0 {
				return false
			}
			i++
		default:
			return false
		}
	}
	return true
}

// isNameChar matches tag-name bytes: [a-zA-Z0-9_.-].
func isNameChar(b byte) bool {
	return isIdentPart(b) || b == '.' || b == '-'
}

// isAttrNameChar matches attribute-name bytes: [a-zA-Z0-9_:.@-]. The dot lets a
// shorthand name a path (<If post.featured>); '@' and '.' let Alpine attributes
// ride on a component tag (@click.prevent) into its attributes bag.
func isAttrNameChar(b byte) bool {
	return isIdentPart(b) || b == ':' || b == '-' || b == '.' || b == '@'
}
