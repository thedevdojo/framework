// Package scanner turns Blade-subset source into a flat token stream.
//
// It is the byte-faithful front of the Pocketknife engine: it recognizes only
// the closed Blade dialect (echoes, raw echoes, comments, directives, and
// <x-…> component/slot tags) and emits everything else as opaque Text spans,
// verbatim. Malformed or unterminated constructs never error — they degrade to
// Text. The scanner never panics on arbitrary input and imports only the Go
// standard library.
//
// Positions are 1-based line AND column, counted in BYTES: a column advances by
// one per byte, and a '\n' increments the line and resets the column to 1.
// Every token records the Pos of its first source byte.
package scanner

import (
	"bytes"
	"strings"
)

// Kind enumerates the token categories the parser and renderer compile against.
type Kind int

const (
	Text      Kind = iota // opaque bytes, emitted verbatim
	Echo                  // {{ expr }}
	RawEcho               // {!! expr !!}
	Comment               // {{-- ... --}}
	Directive             // @word (with optional balanced-paren args)
	TagOpen               // <x-name ...> or <x-name .../>
	TagClose              // </x-name>
	SlotOpen              // <x-slot:name>
	SlotClose             // </x-slot>
	// Error is a construct the scanner recognized but could not turn into a
	// token the parser can act on (an HTML-syntax <Layout> with no src, an
	// <If> with no condition). Name holds the construct's label, Text the
	// message; the parser reports it and moves on. The Blade scanner never
	// emits one — malformed Blade degrades to Text.
	Error
)

// Pos is a 1-based source position; Col counts bytes.
type Pos struct {
	File      string
	Line, Col int
}

// Attr is a single component attribute. Bound marks a :name="expr" binding
// (the leading ':' is stripped from Name).
type Attr struct {
	Name, Value string
	Bound       bool
	// Valueless marks an attribute written with no `=` at all (<x-btn wide/>).
	// Real Blade's tag compiler gives such an attribute the value bool TRUE,
	// which is distinct from an explicit empty value (wide="") — the renderer
	// needs the difference to build props and the $attributes bag faithfully.
	Valueless bool
	Pos       Pos
}

// Token is one lexical unit.
type Token struct {
	Kind Kind
	// Text holds: Text -> the raw bytes; Echo/RawEcho -> the trimmed inner
	// expression; Comment -> "" (content is discarded downstream); Directive ->
	// the trimmed parenthesized args without the outer parens ("" if none).
	Text string
	// Name holds: Directive -> lowercase directive name; TagOpen/TagClose ->
	// component name including dots ("sections.hero"); SlotOpen -> slot name.
	Name string
	// Src holds, for Directive tokens ONLY, the ORIGINAL source slice from the
	// '@' through the last byte the token consumed (the name plus any optional
	// parenthesized args, including whitespace between name and '(' and inside
	// the args, in original casing). It is "" for every other token kind. The
	// renderer writes it verbatim for directives outside the closed subset so an
	// unregistered directive (@media, @keyframes, …) survives byte-for-byte, as
	// it does in real Blade.
	Src       string
	Attrs     []Attr
	SelfClose bool
	Pos       Pos
	// Tag is set ONLY by the HTML-syntax scanner (blade/htmlscan): the tag name
	// exactly as the author wrote it (If, ForEach, SectionsHero, layout). It is
	// "" for every Blade-syntax token. Label uses it so a diagnostic names the
	// construct the way the file spells it.
	Tag string
	// Candidates is set ONLY by the HTML-syntax scanner, on a TagOpen: the
	// component files the tag may name, as paths relative to
	// resources/views/components without an extension, in the order to try
	// them. A Blade <x-name> has exactly one path, derived from Name.
	Candidates []string
}

// Label is the construct's spelling for diagnostics: "@if" or "<x-hero>" for a
// Blade-syntax token, "<If>" or "</SectionsHero>" for an HTML-syntax one.
func (t Token) Label() string {
	if t.Tag != "" {
		switch {
		case t.Kind == TagClose, t.Kind == SlotClose:
			return "</" + t.Tag + ">"
		case t.Kind == Directive && strings.HasPrefix(t.Name, "end"):
			return "</" + t.Tag + ">"
		case t.Kind == SlotOpen:
			return "<" + t.Tag + ` name="` + t.Name + `">`
		}
		return "<" + t.Tag + ">"
	}
	switch t.Kind {
	case Directive:
		return "@" + t.Name
	case TagOpen, TagClose:
		return "<x-" + t.Name + ">"
	case SlotOpen:
		return "<x-slot:" + t.Name + ">"
	case SlotClose:
		return "<x-slot>"
	}
	return ""
}

// Scan tokenizes src in a single forward pass. It never panics and never
// returns an error: unrecognized or malformed input becomes Text.
func Scan(file string, src []byte) []Token {
	s := &scanState{file: file, src: src, line: 1, col: 1}
	for !s.eof() {
		switch {
		case s.has("@@") && s.atBoundary():
			s.escapeAt()
		case s.has("@{{"):
			s.escapeBraces()
		case s.has("{{--"):
			s.comment()
		case s.has("{!!"):
			s.echo("{!!", "!!}", RawEcho)
		case s.has("{{"):
			s.echo("{{", "}}", Echo)
		case s.has("</x-"):
			s.closeTag()
		case s.has("<x-slot:"):
			s.slotOpen()
		case s.has("<x-"):
			s.tagOpen()
		case s.atDirective():
			s.directive()
		default:
			s.text()
		}
	}
	return s.tokens
}

type scanState struct {
	file   string
	src    []byte
	pos    int // byte offset of the cursor
	line   int
	col    int
	tokens []Token
}

func (s *scanState) eof() bool { return s.pos >= len(s.src) }

func (s *scanState) has(prefix string) bool {
	return bytes.HasPrefix(s.src[s.pos:], []byte(prefix))
}

func (s *scanState) curPos() Pos { return Pos{File: s.file, Line: s.line, Col: s.col} }

// adv consumes one byte, advancing the line/column cursor.
func (s *scanState) adv() {
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

// advanceTo consumes bytes until the cursor reaches target (or EOF).
func (s *scanState) advanceTo(target int) {
	for s.pos < target && !s.eof() {
		s.adv()
	}
}

func (s *scanState) save() (int, int, int) { return s.pos, s.line, s.col }

func (s *scanState) restore(p, l, c int) { s.pos, s.line, s.col = p, l, c }

func (s *scanState) readWhile(pred func(byte) bool) string {
	start := s.pos
	for !s.eof() && pred(s.src[s.pos]) {
		s.adv()
	}
	return string(s.src[start:s.pos])
}

func (s *scanState) skipSpaces() {
	for !s.eof() && isSpace(s.src[s.pos]) {
		s.adv()
	}
}

// emitText appends text at pos, coalescing with a preceding Text token so
// contiguous runs collapse into one span.
func (s *scanState) emitText(pos Pos, text string) {
	if text == "" {
		return
	}
	if n := len(s.tokens); n > 0 && s.tokens[n-1].Kind == Text {
		s.tokens[n-1].Text += text
		return
	}
	s.tokens = append(s.tokens, Token{Kind: Text, Text: text, Pos: pos})
}

// emitToEOF emits everything from the cursor to end-of-input as Text. The
// cursor must already sit at the run's first byte (pos captured there).
func (s *scanState) emitToEOF(pos Pos) {
	start := s.pos
	s.advanceTo(len(s.src))
	s.emitText(pos, string(s.src[start:]))
}

// text consumes a run of opaque bytes: the current byte unconditionally, then
// up to (but not including) the next possible construct start.
func (s *scanState) text() {
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

// escapeAt handles "@@": "@@" + identifier -> literal "@word"; a bare "@@" not
// followed by an identifier stays literal "@@".
func (s *scanState) escapeAt() {
	pos := s.curPos()
	var next byte
	if s.pos+2 < len(s.src) {
		next = s.src[s.pos+2]
	}
	if !isIdentStart(next) {
		s.adv() // '@'
		s.adv() // '@'
		s.emitText(pos, "@@")
		return
	}
	s.adv() // '@'
	s.adv() // '@'
	word := s.readWhile(isIdentPart)
	s.emitText(pos, "@"+word)
}

// escapeBraces handles "@{{": the '@' is dropped and the whole brace construct
// through its matching "}}" becomes Text. Unterminated -> the raw source from
// '@' onward is emitted verbatim.
func (s *scanState) escapeBraces() {
	pos := s.curPos()
	close := bytes.Index(s.src[s.pos+3:], []byte("}}"))
	if close < 0 {
		s.emitToEOF(pos) // includes the '@'
		return
	}
	end := s.pos + 3 + close + 2 // past the closing "}}"
	text := string(s.src[s.pos+1 : end])
	s.advanceTo(end)
	s.emitText(pos, text)
}

// comment handles "{{-- ... --}}". Unterminated -> the opening delimiter is
// treated as literal Text and scanning continues.
func (s *scanState) comment() {
	pos := s.curPos()
	close := bytes.Index(s.src[s.pos+4:], []byte("--}}"))
	if close < 0 {
		s.emitDelim(pos, 4) // "{{--" as text, then continue
		return
	}
	end := s.pos + 4 + close + 4
	s.advanceTo(end)
	s.tokens = append(s.tokens, Token{Kind: Comment, Pos: pos})
}

// echo handles {{ … }} and {!! … !!}. Unterminated -> the opening delimiter is
// treated as literal Text and scanning continues.
func (s *scanState) echo(open, close string, kind Kind) {
	pos := s.curPos()
	rel := bytes.Index(s.src[s.pos+len(open):], []byte(close))
	if rel < 0 {
		s.emitDelim(pos, len(open))
		return
	}
	innerStart := s.pos + len(open)
	innerEnd := innerStart + rel
	inner := strings.TrimSpace(string(s.src[innerStart:innerEnd]))
	s.advanceTo(innerEnd + len(close))
	s.tokens = append(s.tokens, Token{Kind: kind, Text: inner, Pos: pos})
}

// emitDelim emits the first n bytes at the cursor as literal Text and advances
// past them, so a later construct after an unterminated opener still scans.
func (s *scanState) emitDelim(pos Pos, n int) {
	start := s.pos
	s.advanceTo(s.pos + n)
	s.emitText(pos, string(s.src[start:s.pos]))
}

// directive handles @word with optional balanced-paren args. Unterminated args
// -> the whole run from '@' becomes Text.
func (s *scanState) directive() {
	pos := s.curPos()
	sp, sl, sc := s.save()
	s.adv() // '@'
	name := strings.ToLower(s.readWhile(isIdentPart))

	// Args only if '(' is the next non-space byte; that whitespace, if any, is
	// consumed as part of the directive (not emitted as Text).
	ap, al, ac := s.save()
	s.skipSpaces()
	if s.eof() || s.src[s.pos] != '(' {
		s.restore(ap, al, ac) // trailing whitespace stays as content
		// Src spans '@' (sp) through the name's last byte (the cursor after the
		// restore) — no args consumed.
		s.tokens = append(s.tokens, Token{Kind: Directive, Name: name, Src: string(s.src[sp:s.pos]), Pos: pos})
		return
	}
	inner, ok := s.captureParens()
	if !ok {
		s.restore(sp, sl, sc)
		s.emitToEOF(pos)
		return
	}
	// Src spans '@' (sp) through the closing ')' the cursor now sits just past,
	// so any whitespace between the name and '(' and every inner byte is kept.
	s.tokens = append(s.tokens, Token{Kind: Directive, Name: name, Text: strings.TrimSpace(inner), Src: string(s.src[sp:s.pos]), Pos: pos})
}

// captureParens consumes a balanced-paren group starting at the current '(',
// honoring '…' and "…" string literals (with \' / \" escapes). It returns the
// inner content (without the outer parens) and whether it was terminated.
func (s *scanState) captureParens() (string, bool) {
	s.adv() // '('
	start := s.pos
	depth := 1
	for !s.eof() {
		b := s.src[s.pos]
		switch b {
		case '\'', '"':
			q := b
			s.adv()
			for !s.eof() {
				c := s.src[s.pos]
				if c == '\\' {
					s.adv()
					s.adv() // escaped byte (adv is EOF-safe)
					continue
				}
				s.adv()
				if c == q {
					break
				}
			}
		case '(':
			depth++
			s.adv()
		case ')':
			depth--
			s.adv()
			if depth == 0 {
				return string(s.src[start : s.pos-1]), true
			}
		default:
			s.adv()
		}
	}
	return "", false
}

// tagOpen handles <x-name …> / <x-name …/>. Empty name -> the '<' is literal;
// unterminated (EOF before '>') -> the whole run from '<' becomes Text.
func (s *scanState) tagOpen() {
	pos := s.curPos()
	p, l, c := s.save()
	s.advanceTo(s.pos + 3) // "<x-"
	name := s.readWhile(isNameChar)
	if name == "" {
		s.literalLt(pos, p, l, c)
		return
	}
	attrs, selfClose, ok := s.lexAttrs()
	if !ok {
		s.restore(p, l, c)
		s.emitToEOF(pos)
		return
	}
	s.tokens = append(s.tokens, Token{Kind: TagOpen, Name: name, Attrs: attrs, SelfClose: selfClose, Pos: pos})
}

// slotOpen handles <x-slot:name …>. Malformed -> the '<' is literal Text.
func (s *scanState) slotOpen() {
	pos := s.curPos()
	p, l, c := s.save()
	s.advanceTo(s.pos + 8) // "<x-slot:"
	name := s.readWhile(isNameChar)
	if name == "" {
		s.literalLt(pos, p, l, c)
		return
	}
	attrs, selfClose, ok := s.lexAttrs()
	if !ok {
		s.restore(p, l, c)
		s.emitToEOF(pos)
		return
	}
	s.tokens = append(s.tokens, Token{Kind: SlotOpen, Name: name, Attrs: attrs, SelfClose: selfClose, Pos: pos})
}

// closeTag handles </x-name> and </x-slot>. Malformed -> the '<' is literal.
func (s *scanState) closeTag() {
	pos := s.curPos()
	p, l, c := s.save()
	s.advanceTo(s.pos + 4) // "</x-"
	name := s.readWhile(isNameChar)
	if name == "" {
		s.literalLt(pos, p, l, c)
		return
	}
	s.skipSpaces()
	if s.eof() || s.src[s.pos] != '>' {
		s.literalLt(pos, p, l, c)
		return
	}
	s.adv() // '>'
	if name == "slot" {
		s.tokens = append(s.tokens, Token{Kind: SlotClose, Pos: pos})
		return
	}
	s.tokens = append(s.tokens, Token{Kind: TagClose, Name: name, Pos: pos})
}

// literalLt rewinds to a saved cursor, emits the single '<' as literal Text,
// and leaves the cursor just past it so scanning recovers on the next byte.
func (s *scanState) literalLt(pos Pos, p, l, c int) {
	s.restore(p, l, c)
	s.adv() // '<'
	s.emitText(pos, "<")
}

// lexAttrs reads component attributes until '>' or '/>'. It returns the
// attributes, whether the tag self-closes, and false if EOF arrives first.
func (s *scanState) lexAttrs() (attrs []Attr, selfClose, ok bool) {
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
			return nil, false, false // stray '/'
		}
		aPos := s.curPos()
		raw := s.readWhile(isAttrNameChar)
		if raw == "" {
			return nil, false, false // unexpected byte
		}
		bound := false
		name := raw
		switch {
		case strings.HasPrefix(raw, "::"):
			// `::foo` is real Blade's escape for a literal `:foo` attribute (an
			// Alpine/Vue binding): the tag compiler strips one colon and treats
			// the attribute as STATIC (ComponentTagCompiler, "attributes that
			// start with :: are compiled with a single :").
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
				s.adv() // opening quote
				vStart := s.pos
				for !s.eof() && s.src[s.pos] != q {
					s.adv()
				}
				if s.eof() {
					return nil, false, false // unterminated value
				}
				value = string(s.src[vStart:s.pos])
				s.adv() // closing quote
			} else {
				vStart := s.pos
				for !s.eof() && !isSpace(s.src[s.pos]) && s.src[s.pos] != '>' && s.src[s.pos] != '/' {
					s.adv()
				}
				value = string(s.src[vStart:s.pos])
			}
		}
		attrs = append(attrs, Attr{Name: name, Value: value, Bound: bound, Valueless: valueless, Pos: aPos})
	}
}

func (s *scanState) atDirective() bool {
	return s.atBoundary() && s.src[s.pos] == '@' && s.pos+1 < len(s.src) && isIdentStart(s.src[s.pos+1])
}

// atBoundary reports whether the cursor sits at a word boundary for '@'
// recognition. Real Blade compiles directives (and the @@ escape) with a \B@
// pattern: an '@' only starts a directive when the preceding SOURCE byte is not
// a word char ([A-Za-z0-9_]); start-of-input counts as a boundary. This keeps
// mailto addresses (hello@example.com) and text like foo@endif verbatim. The
// @{{ echo escape is deliberately exempt — real Blade's echo regex has no \B —
// so escapeBraces is dispatched without this guard. The previous byte is read
// from the source (s.pos-1), so text coalescing and save/restore rewinds cannot
// skew it.
func (s *scanState) atBoundary() bool {
	return s.pos == 0 || !isIdentPart(s.src[s.pos-1])
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// isNameChar matches component/slot name bytes: [a-zA-Z0-9._-].
func isNameChar(b byte) bool {
	return isIdentPart(b) || b == '.' || b == '-'
}

// isAttrNameChar matches attribute-name bytes: [:a-zA-Z0-9_.@-]. The dot and
// the '@' are what Alpine and Livewire attributes are made of (@click.prevent,
// x-on:keydown.escape, wire:model.live); real Blade's tag compiler accepts
// them and passes the attribute into the bag under that exact name.
func isAttrNameChar(b byte) bool {
	return isIdentPart(b) || b == ':' || b == '-' || b == '.' || b == '@'
}
