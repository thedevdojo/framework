// Package htmlscan is the scanner for the HTML syntax: DevDojo Framework's
// second spelling of the template language, used by .html files.
//
// The HTML syntax is Blade written as tags. A capitalized tag is either one of
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
//   - {expr}, a value in single braces, and \{ for a brace that is text,
//     wherever a brace is read (see Context): text and the values of
//     attributes that do not hold code. Only an inside that parses as an
//     expression counts; { open: false } and {} stay text.
//   - {{ expr }} (still read, with a notice that devdojo migrate rewrites
//     it), {!! expr !!}, {{-- comment --}}, and the old @{{ escape
//   - a tag whose name starts with a capital letter (a real HTML element
//     shouted in ALL CAPS, <BR> or <TABLE>, is still HTML)
//   - <layout> and <include> in lowercase, the DevDojo Static spellings
//
// Everything else is opaque text, emitted verbatim: plain HTML, an Alpine
// @click, a CSS @media, an email address. There are no @directives. An HTML
// comment is opaque too, so commenting a block out really switches it off.
// Inside <script> and <style> a capitalized tag is treated as code rather than
// a component (a<B>c), and a single brace is the code's own, though the
// built-in tags and the double-brace forms still work.
//
// Like the Blade scanner it never panics and never returns an error: a tag it
// cannot read degrades to text, and a tag it can read but that is missing
// something it needs becomes a scanner.Error token carrying the message.
// Positions are 1-based line and column in bytes.
package htmlscan

import (
	"bytes"
	"strings"

	"github.com/thedevdojo/framework/blade/expr"
	"github.com/thedevdojo/framework/blade/scanner"
)

// Scan tokenizes src in a single forward pass.
func Scan(file string, src []byte) []scanner.Token {
	toks, _ := ScanWith(file, src, Options{})
	return toks
}

// Options change how ScanWith reads a file.
type Options struct {
	// Legacy reads the file the way the engine did before single braces:
	// {title} and \{ are text everywhere. devdojo migrate needs it to know
	// what an older site printed.
	Legacy bool
}

// Info is what a scan learns about a file beside its tokens.
type Info struct {
	// DoubleBraces are the {{ … }} values, and the @{{ … }} escapes, written
	// where a single brace would do: in text and in an attribute that holds
	// no code. They still work; `devdojo migrate` rewrites them. Each is the
	// position of its first brace, or of the component attribute holding it.
	DoubleBraces []scanner.Pos
	// NearMisses are single braces where a brace is read whose inside reads
	// as an attempt at a value the engine cannot compute: a function or
	// method call ({now()}, {post.url()}). They print as text, as written,
	// and lint says so.
	NearMisses []scanner.Pos
}

// ScanWith tokenizes src as Scan does, with options, and says what it learned.
func ScanWith(file string, src []byte, opts Options) ([]scanner.Token, Info) {
	s := &state{file: file, src: src, line: 1, col: 1, open: map[string][]string{}, legacy: opts.Legacy}
	for !s.eof() {
		s.step()
	}
	return s.tokens, s.info
}

// step reads one construct, or one run of text, at the cursor.
func (s *state) step() {
	switch {
	case s.has("@{{"):
		s.escapeBraces()
	case s.has("{{--"):
		s.comment()
	case s.has("{!!"):
		s.echo("{!!", "!!}", scanner.RawEcho)
	case s.has("{{"):
		s.echo("{{", "}}", scanner.Echo)
	case !s.legacy && s.has(`\{`) && s.ctx.Reads():
		// \{ is a brace and nothing more.
		pos := s.curPos()
		s.adv()
		s.adv()
		s.emitText(pos, "{")
	case !s.legacy && s.src[s.pos] == '{' && s.ctx.Reads() && s.single():
	case s.ctx.elem == "" && s.has("<!--"):
		s.htmlComment()
	case s.has("</"):
		s.closeTag()
	case s.src[s.pos] == '<':
		s.openTag()
	default:
		s.text()
	}
}

type state struct {
	file   string
	src    []byte
	pos    int // byte offset of the cursor
	line   int
	col    int
	tokens []scanner.Token
	// ctx is where the cursor stands as a browser reads the page, which says
	// whether a single brace is the template's. Its Elem is the <script> or
	// <style> the cursor is inside, where a capitalized tag is code rather
	// than a component.
	ctx Context
	// open tracks, per paired built-in tag, what each still-open tag resolved
	// to, innermost last. A closing tag pops its entry: a <Layout> learns the
	// component name its opener resolved from src, and "" marks an opener that
	// was refused with an Error token, whose closer is then swallowed so the
	// mistake is reported once.
	open map[string][]string
	// legacy reads no single braces (Options.Legacy).
	legacy bool
	info   Info
	// brace caches the offset of the next '{' or '}' after the cursor (or
	// len(src) for none), so finding where a single brace closes costs one
	// pass over the file however many braces it holds.
	brace int
	// spans are the attributes of the tag lexAttrs read last, as written.
	spans []attrSpan
}

// attrSpan is where one attribute's name and value sit in src.
type attrSpan struct {
	name       string // as written, with any ':' or '::'
	hasValue   bool
	start, end int // the value, between its quotes
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

// emit appends a token. A construct of the template's own moves the context
// past it as one byte; a comment prints nothing and does not.
func (s *state) emit(tok scanner.Token) {
	if tok.Kind != scanner.Text && tok.Kind != scanner.Comment {
		s.ctx.Construct()
	}
	s.tokens = append(s.tokens, tok)
}

// emitText appends text at pos, coalescing with a preceding Text token.
func (s *state) emitText(pos scanner.Pos, text string) {
	if text == "" {
		return
	}
	s.ctx.Feed(text)
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
		case '{', '<', '@', '\\':
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
	if !s.legacy && s.ctx.Reads() {
		s.info.DoubleBraces = append(s.info.DoubleBraces, pos)
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
	if kind == scanner.Echo && !s.legacy && s.ctx.Reads() && SingleBrace(inner) {
		s.info.DoubleBraces = append(s.info.DoubleBraces, pos)
	}
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
		if s.ctx.elem == "" {
			s.ctx.elem = lower
		}
		s.literalLt(pos, p, l, c)
		return
	case tagHTML:
		s.literalLt(pos, p, l, c)
		return
	case tagComponent:
		if s.ctx.elem != "" {
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
		Attrs: s.componentAttrs(attrs), SelfClose: selfClose, Pos: pos,
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
	if class == tagRawText && lower == s.ctx.elem {
		s.ctx.elem = ""
	}
	if class == tagHTML || class == tagRawText || (class == tagComponent && s.ctx.elem != "") {
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
	s.spans = s.spans[:0]
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
		span := attrSpan{name: raw}
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
				span.hasValue, span.start, span.end = true, vStart, s.pos
				s.adv()
			} else {
				vStart := s.pos
				for !s.eof() && !isSpace(s.src[s.pos]) && s.src[s.pos] != '>' && s.src[s.pos] != '/' {
					s.adv()
				}
				value = string(s.src[vStart:s.pos])
				span.hasValue, span.start, span.end = true, vStart, s.pos
			}
		}
		s.spans = append(s.spans, span)
		attrs = append(attrs, scanner.Attr{Name: name, Value: value, Bound: bound, Valueless: valueless, Pos: aPos})
	}
}

// componentAttrs prepares a component tag's attributes for the renderer: a
// bound attribute's expression is rewritten to Blade form, and a text value
// that embeds a value — {name}, or the older {{ name }} — becomes the bound
// attribute it is sugar for. An attribute that holds code (x-text, @click,
// ::class, onclick) keeps its single braces as text, as it does on plain HTML.
func (s *state) componentAttrs(attrs []scanner.Attr) []scanner.Attr {
	for i := range attrs {
		a := &attrs[i]
		switch {
		case a.Bound:
			a.Value = Expr(a.Value)
		case !a.Valueless:
			single := !s.legacy && !IsCodeAttr(a.Name)
			if single && hasDoubleBraces(a.Value) {
				s.info.DoubleBraces = append(s.info.DoubleBraces, a.Pos)
			}
			joined, ok := interpolate(a.Value, single)
			a.Value, a.Bound = joined, ok
		}
	}
	return attrs
}

// hasDoubleBraces reports whether a value embeds a {{ … }} that {…} would do.
func hasDoubleBraces(value string) bool {
	for rest := value; ; {
		i := strings.Index(rest, "{{")
		if i < 0 {
			return false
		}
		end := strings.Index(rest[i+2:], "}}")
		if end < 0 {
			return false
		}
		if SingleBrace(strings.TrimSpace(rest[i+2 : i+2+end])) {
			return true
		}
		rest = rest[i+2+end+2:]
	}
}

// interpolate turns a text attribute value that embeds values into the
// expression that joins its pieces, and reports whether it embeds any:
//
//	Hi {name}!   →   'Hi ' . $name . '!'
//
// title="Hi {name}" is therefore exactly :title="'Hi ' . name" — one meaning,
// two spellings, and the form Blade can say too. The result is always text: a
// value standing alone is joined onto an empty string so it is not passed as a
// number or a list. {{ … }} and {!! … !!} read the same as {…} here, since an
// attribute's value is escaped when it is printed, never when it is passed.
//
// With single false (an attribute holding code, or a legacy read) only
// {{ … }} and {!! … !!} are values, and an opener left unterminated leaves the
// rest of the value as written. With single true a brace is read as it is in
// text: \{ is a brace, {value} is a value, and an unterminated opener is text.
// For a value that embeds nothing, the text it means is returned, with ok
// false.
func interpolate(value string, single bool) (string, bool) {
	if !strings.Contains(value, "{") {
		return value, false
	}
	var parts []string
	var text strings.Builder
	exprs := 0
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, phpString(text.String()))
			text.Reset()
		}
	}
	add := func(inner string) {
		flush()
		inner = Expr(strings.TrimSpace(inner))
		if !isAtom(inner) {
			inner = "(" + inner + ")"
		}
		parts = append(parts, inner)
		exprs++
	}
	for i := 0; i < len(value); {
		rest := value[i:]
		open, close := "", ""
		switch {
		case strings.HasPrefix(rest, "{!!"):
			open, close = "{!!", "!!}"
		case strings.HasPrefix(rest, "{{"):
			open, close = "{{", "}}"
		}
		if open != "" {
			end := strings.Index(rest[len(open):], close)
			switch {
			case end >= 0:
				add(rest[len(open) : len(open)+end])
				i += len(open) + end + len(close)
			case single:
				text.WriteString(open)
				i += len(open)
			default:
				text.WriteString(rest)
				i = len(value)
			}
			continue
		}
		if single && strings.HasPrefix(rest, `\{`) {
			text.WriteByte('{')
			i += 2
			continue
		}
		if single && rest[0] == '{' {
			if inner, ok := singleAt(rest); ok {
				add(inner)
				i += len(inner) + 2
				continue
			}
		}
		text.WriteByte(value[i])
		i++
	}
	if exprs == 0 {
		return text.String(), false
	}
	flush()
	if len(parts) == 1 {
		parts = []string{"''", parts[0]}
	}
	return strings.Join(parts, " . "), true
}

// single reads a value in single braces at the cursor, {title}, and reports
// whether there was one. Nothing is consumed when the braces do not hold an
// expression: then the brace is text.
func (s *state) single() bool {
	if s.brace <= s.pos {
		s.brace = len(s.src)
		if j := bytes.IndexAny(s.src[s.pos+1:], "{}"); j >= 0 {
			s.brace = s.pos + 1 + j
		}
	}
	if s.brace >= len(s.src) || s.src[s.brace] != '}' {
		return false
	}
	inner := strings.TrimSpace(string(s.src[s.pos+1 : s.brace]))
	if !SingleBrace(inner) {
		if inner != "" && expr.Call(Expr(inner)) {
			s.info.NearMisses = append(s.info.NearMisses, s.curPos())
		}
		return false
	}
	pos := s.curPos()
	s.advanceTo(s.brace + 1)
	if name, args, ok := classCall(inner); ok {
		s.emit(scanner.Token{Kind: scanner.Directive, Name: name, Text: Expr(args), Pos: pos})
		return true
	}
	s.emit(scanner.Token{Kind: scanner.Echo, Text: Expr(inner), Pos: pos})
	return true
}

// SingleBrace reports whether inner, the trimmed text between a pair of
// single braces, is a value: an expression the engine evaluates, or a
// class([…]) or style([…]) call. Nothing with a brace in it is.
func SingleBrace(inner string) bool {
	if inner == "" || strings.ContainsAny(inner, "{}") {
		return false
	}
	if _, args, ok := classCall(inner); ok {
		return args == "" || expr.Valid(Expr(args))
	}
	return expr.Valid(Expr(inner))
}

// SingleEcho reports whether a friendly expression can be written as a value
// in single braces, {friendly}, and be read back as that same echo: it is a
// value, it is not a class() or style() call (those are directives), and it
// does not start with "!!" ({!! would open a raw echo).
func SingleEcho(friendly string) bool {
	if friendly != strings.TrimSpace(friendly) || strings.HasPrefix(friendly, "!!") {
		return false
	}
	if _, _, ok := classCall(friendly); ok {
		return false
	}
	return SingleBrace(friendly)
}

// singleAt reads the value in single braces that s starts with: the text up
// to the next brace, when that brace closes and what it holds is a value.
func singleAt(s string) (inner string, ok bool) {
	j := strings.IndexAny(s[1:], "{}")
	if j < 0 || s[1+j] != '}' {
		return "", false
	}
	inner = s[1 : 1+j]
	return inner, SingleBrace(strings.TrimSpace(inner))
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
