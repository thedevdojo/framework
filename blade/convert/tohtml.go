package convert

import (
	"strings"

	"github.com/thedevdojo/framework/blade/htmlscan"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/blade/scanner"
)

// printHTML writes a Blade file's tokens in the Studio syntax.
//
// Where a construct has several HTML spellings the shortest one that scans
// back to the same token wins — <If post.featured> over
// <If condition="post.featured">, <SectionsHero> over <Include src="…">.
// Each such choice is made by trying it, not by reasoning about it.
//
// tidy also takes out what only Blade needed: a character printed through a
// raw echo to keep Laravel from reading it ({!! '@' !!}media), an empty echo
// holding two things apart, and the @break that stands in for a loop's limit.
// Those are what a page converted from the Studio syntax picks up on the way to
// Blade, so tidying is what returns it as it was written. File falls back to
// the untidied print if the tidy one does not read back the same.
func printHTML(path string, src []byte, toks []scanner.Token, exists func(string) bool, tidy bool) (string, []render.Diagnostic) {
	p := &htmlPrinter{path: path, off: newOffsets(src), exists: exists, tidy: tidy}
	toks = p.settle(toks)
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		switch tok.Kind {
		case scanner.Text:
			// Literal defuses whatever the HTML scanner would take for its
			// own — a capitalized tag, an HTML comment left open — and tracks
			// whether the text has walked into a <script> or <style>.
			lit, raw := htmlscan.Literal(tok.Text, p.raw)
			p.out.write(lit)
			p.raw = raw
		case scanner.Echo:
			if tok.Text == "$slot" {
				p.out.write("<Slot />")
				continue
			}
			p.brace("{{ " + htmlscan.Friendly(tok.Text) + " }}")
		case scanner.RawEcho:
			p.brace("{!! " + htmlscan.Friendly(tok.Text) + " !!}")
		case scanner.Comment:
			p.brace(p.off.commentSource(tok.Pos))
		case scanner.Directive:
			if tok.Name == "foreach" {
				limit, skip := "", 0
				if p.tidy {
					limit, skip = loopLimitBreak(toks, i)
				}
				p.foreach(tok, limit)
				i += skip
				continue
			}
			p.directive(tok)
		case scanner.TagOpen:
			p.tagOpen(tok)
		case scanner.TagClose:
			form, _ := p.form(tok.Name)
			p.out.write("</" + form.close + ">")
		case scanner.SlotOpen:
			p.out.write(`<Slot name="` + tok.Name + `">`)
		case scanner.SlotClose:
			p.out.write("</Slot>")
		}
	}
	return p.out.String(), p.problems
}

type htmlPrinter struct {
	path     string
	off      offsets
	exists   func(string) bool
	tidy     bool // take out the escapes only Blade needed
	out      buffer
	raw      string // the <script> or <style> the output is inside, if any
	problems []render.Diagnostic
}

func (p *htmlPrinter) problem(pos scanner.Pos, msg string) {
	p.problems = append(p.problems, problem(p.path, pos.Line, pos.Col, msg))
}

// brace writes a construct that opens with a brace. Text ending in '{' or '@'
// would fuse with it ("@{{" is the escape, "{{{" opens one brace early), so
// such a last byte is reprinted through a hatch first.
func (p *htmlPrinter) brace(s string) {
	p.out.hatchLast("{@")
	p.out.write(s)
}

// engineDirectives are the directives this engine renders itself. Every other
// @word is either Laravel's (refused) or nobody's (plain text).
var engineDirectives = map[string]bool{
	"if": true, "elseif": true, "else": true, "endif": true,
	"foreach": true, "endforeach": true, "break": true, "continue": true,
	"props": true, "vite": true, "class": true, "style": true,
}

// settle turns every directive outside the engine's subset into what it is to
// an .html file. One Laravel compiles (@auth) is a problem: the page needs
// Laravel. One nobody compiles (@media, @handle) is the text it prints, and
// joins the text around it so an HTML comment spanning it stays in one piece.
//
// When tidying, a hatch becomes the character it prints — text like any other,
// which Literal hatches again if the Studio syntax needs it too — and a separator
// is dropped wherever it only held two things apart.
func (p *htmlPrinter) settle(toks []scanner.Token) []scanner.Token {
	out := make([]scanner.Token, 0, len(toks))
	for i, tok := range toks {
		if tok.Kind == scanner.Directive && !engineDirectives[tok.Name] {
			if laravelDirectives[tok.Name] {
				p.problem(tok.Pos, "@"+tok.Name+" needs real Laravel, which "+Studio.Name()+" does not have")
			}
			tok = scanner.Token{Kind: scanner.Text, Text: tok.Src, Pos: tok.Pos}
		}
		if p.tidy && tok.Kind == scanner.RawEcho {
			if c, ok := hatched(tok.Text); ok {
				tok = scanner.Token{Kind: scanner.Text, Text: string(c), Pos: tok.Pos}
			}
		}
		if p.tidy && tok.Kind == scanner.Echo && tok.Text == "''" {
			afterText := len(out) > 0 && out[len(out)-1].Kind == scanner.Text
			if afterText || !holdsLineBreak(toks, i) {
				continue
			}
		}
		if n := len(out); tok.Kind == scanner.Text && n > 0 && out[n-1].Kind == scanner.Text {
			out[n-1].Text += tok.Text
			continue
		}
		out = append(out, tok)
	}
	return out
}

// holdsLineBreak reports whether the separator at toks[i] may be all that
// keeps a line break on the page. A directive or a component tag swallows the
// line break that follows it; an echo does not. So a separator standing before
// a line break (or before a comment, which is not there once compiled) stays
// unless plain text comes before it.
func holdsLineBreak(toks []scanner.Token, i int) bool {
	if i+1 >= len(toks) {
		return false
	}
	next := toks[i+1]
	if next.Kind == scanner.Comment {
		return true
	}
	return next.Kind == scanner.Text && (strings.HasPrefix(next.Text, "\n") || strings.HasPrefix(next.Text, "\r"))
}

// loopLimitBreak recognizes the @break that stands in for a loop's limit —
// @break($loop->index >= N), first thing in the loop at toks[i] — and returns
// the limit with the number of tokens after the @foreach that it replaces.
// It is the reverse of what a <Foreach limit> becomes in Blade, and applies
// under the same conditions (see limitRefusal).
func loopLimitBreak(toks []scanner.Token, i int) (limit string, skip int) {
	j := i + 1
	if j+1 < len(toks) && toks[j].Kind == scanner.Text && toks[j].Text == "\n" {
		j++ // the break on its own line
	}
	if j >= len(toks) || toks[j].Kind != scanner.Directive || toks[j].Name != "break" {
		return "", 0
	}
	limit, ok := strings.CutPrefix(toks[j].Text, "$loop->index >= ")
	if !ok {
		return "", 0
	}
	if len(limit) > 2 && limit[0] == '(' && closes(limit) && !simpleOperand(limit[1:len(limit)-1]) {
		limit = limit[1 : len(limit)-1]
	}
	if limitCondition(limit) != toks[j].Text || limitRefusal(toks, i, limit) != "" {
		return "", 0
	}
	// On its own line, the break must be followed by a line break too: that
	// is the one the loop's tag will be followed by.
	ownLine := j == i+2
	nextBreaks := j+1 < len(toks) && toks[j+1].Kind == scanner.Text && strings.HasPrefix(toks[j+1].Text, "\n")
	if ownLine != nextBreaks {
		return "", 0
	}
	return limit, j - i
}

func (p *htmlPrinter) directive(tok scanner.Token) {
	switch tok.Name {
	case "if":
		p.out.write("<If" + p.condition(tok) + ">")
	case "elseif":
		p.out.write("<ElseIf" + p.condition(tok) + ">")
	case "else":
		p.out.write("<Else>")
	case "endif":
		p.out.write("</If>")
	case "endforeach":
		p.out.write("</Foreach>")
	case "break", "continue":
		tag := "Break"
		if tok.Name == "continue" {
			tag = "Continue"
		}
		if tok.Text == "" {
			p.out.write("<" + tag + " />")
		} else {
			p.out.write("<" + tag + p.condition(tok) + " />")
		}
	case "props":
		p.props(tok)
	case "vite":
		p.vite(tok)
	case "class", "style":
		p.brace("{{ " + tok.Name + "(" + htmlscan.Friendly(tok.Text) + ") }}")
	}
}

// condition writes a directive's condition as tag attributes, with a leading
// space: the bare name for "is truthy", name="text" for "equals this", and
// condition="…" for everything else.
func (p *htmlPrinter) condition(tok scanner.Token) string {
	friendly := htmlscan.Friendly(tok.Text)
	// reads reports whether an <If> with these attributes means tok's
	// condition, by asking the scanner.
	reads := func(attrs string) bool {
		got := htmlscan.Scan("", []byte("<If "+attrs+">"))
		return len(got) == 1 && got[0].Kind == scanner.Directive && got[0].Text == tok.Text
	}
	if isPath(friendly) && reads(friendly) {
		return " " + friendly
	}
	if left, right, ok := strings.Cut(tok.Text, " == "); ok {
		name := htmlscan.Friendly(left)
		value := right
		if s, isText := stringLiteral(right); isText && right[0] == '\'' {
			value = s
		}
		if attr := name + `="` + value + `"`; isPath(name) && !strings.ContainsAny(value, "\"\n") && reads(attr) {
			return " " + attr
		}
	}
	q, ok := pickQuote(friendly, '"')
	if !ok {
		p.problem(tok.Pos, tok.Label()+" uses both kinds of quotes, which one tag attribute cannot hold; use one kind")
		return ` condition=""`
	}
	return " condition=" + string(q) + friendly + string(q)
}

// foreachWords are the attribute names <Foreach> keeps for itself. A list with
// one of these names takes the long form.
var foreachWords = map[string]bool{"collection": true, "as": true, "key": true, "limit": true, "count": true}

// foreach writes @foreach as <Foreach>, with the limit its leading @break
// stood for when there is one.
func (p *htmlPrinter) foreach(tok scanner.Token, limit string) {
	end := ">"
	if limit != "" {
		limit = htmlscan.Friendly(limit)
		q, fits := pickQuote(limit, '"')
		if !fits {
			p.problem(tok.Pos, "@break uses both kinds of quotes, which one tag attribute cannot hold; use one kind")
			q = '"'
		}
		end = " limit=" + string(q) + limit + string(q) + ">"
	}
	items, key, val, ok := loopParts(tok.Text)
	val, valOK := strings.CutPrefix(val, "$")
	if key != "" {
		var keyOK bool
		key, keyOK = strings.CutPrefix(key, "$")
		valOK = valOK && keyOK && isName(key)
	}
	if !ok || items == "" || !valOK || !isName(val) {
		p.problem(tok.Pos, `@foreach is not in the form "$list as $item", so it cannot be converted`)
		p.out.write("<Foreach>")
		return
	}
	list := htmlscan.Friendly(items)
	if isPath(list) && !foreachWords[list] {
		as := val
		if key != "" {
			as = key + ", " + val
		}
		p.out.write("<Foreach " + list + `="` + as + `"` + end)
		return
	}
	q, quoted := pickQuote(list, '"')
	if !quoted {
		p.problem(tok.Pos, "@foreach uses both kinds of quotes, which one tag attribute cannot hold; use one kind")
		q = '"'
	}
	tag := "<Foreach collection=" + string(q) + list + string(q) + ` as="` + val + `"`
	if key != "" {
		tag += ` key="` + key + `"`
	}
	p.out.write(tag + end)
}

// props writes @props as <Props>: text defaults as plain attributes, an empty
// (null) or required prop as a bare name, anything else — a number, true, a
// list — after a colon, exactly as written.
func (p *htmlPrinter) props(tok scanner.Token) {
	props, ok := propList(tok.Text)
	if !ok {
		p.problem(tok.Pos, "@props is not a plain list of names and defaults, so it cannot be converted")
		p.out.write("<Props />")
		return
	}
	attrs := make([]string, 0, len(props))
	for _, pr := range props {
		if !isPropName(pr.name) {
			p.problem(tok.Pos, `@props name "`+pr.name+`" cannot be written as a tag attribute`)
			continue
		}
		if !pr.hasDefault || pr.value == "null" {
			attrs = append(attrs, pr.name)
			continue
		}
		if s, isText := plainString(pr.value); isText {
			attrs = append(attrs, pr.name+"="+htmlscan.QuoteProp(s))
			continue
		}
		q, fits := pickQuote(pr.value, '"')
		if !fits {
			p.problem(tok.Pos, `@props default for "`+pr.name+`" uses both kinds of quotes, which one tag attribute cannot hold`)
			continue
		}
		attrs = append(attrs, ":"+pr.name+"="+string(q)+pr.value+string(q))
	}
	if strings.Contains(tok.Src, "\n") && len(attrs) > 0 {
		indent := p.off.indent(tok.Pos)
		p.out.write("<Props\n")
		for _, a := range attrs {
			p.out.write(indent + "    " + a + "\n")
		}
		p.out.write(indent + "/>")
		return
	}
	if len(attrs) == 0 {
		p.out.write("<Props />")
		return
	}
	p.out.write("<Props " + strings.Join(attrs, " ") + " />")
}

// isPropName reports whether a prop name can be written as an attribute name
// and read back unchanged.
func isPropName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}

func (p *htmlPrinter) vite(tok scanner.Token) {
	entries, ok := viteEntries(tok.Text)
	if !ok {
		p.problem(tok.Pos, "@vite is not a file name or a plain list of them, so it cannot be converted")
		p.out.write("<Vite />")
		return
	}
	if len(entries) == 0 {
		p.out.write("<Vite />")
		return
	}
	p.out.write(`<Vite src="` + strings.Join(entries, ", ") + `" />`)
}

// tagForm is how a component is written as a Studio-syntax tag: what follows
// the '<' of the opening tag, and the name of the closing one.
type tagForm struct {
	open, close string
}

// form picks the tag for a Blade component name, most readable first:
//
//	layouts.main            <Layout src="main.html">
//	sections.hero           <SectionsHero>
//	pricing-table.row       <PricingTable.Row>   when <PricingTableRow> would
//	                                              find another file first
//	anything else           <Include src="…">    always names exactly one file
//
// A component tag is only used when it resolves to this very file among the
// site's components. Inside <script> and <style> a capitalized tag is code,
// so there the built-in <Include> is the only choice.
func (p *htmlPrinter) form(name string) (tagForm, bool) {
	path := strings.ReplaceAll(name, ".", "/")
	if rest, ok := strings.CutPrefix(path, "layouts/"); ok && isSrc(rest) {
		return tagForm{open: `Layout src="` + rest + `.html"`, close: "Layout"}, true
	}
	if p.raw == "" {
		for _, tag := range []string{pascal(path, ""), pascal(path, ".")} {
			if tag != "" && htmlscan.IsComponentTag(tag) && resolve(htmlscan.Candidates(tag), p.exists) == name {
				return tagForm{open: tag, close: tag}, true
			}
		}
	}
	if isSrc(path) {
		return tagForm{open: `Include src="` + path + `.html"`, close: "Include"}, true
	}
	return tagForm{open: "Include", close: "Include"}, false
}

// isSrc reports whether path can be the src of a <Layout> or <Include> and be
// read back as the same path.
func isSrc(path string) bool {
	if path == "" || strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".blade.php") {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// pascal writes a component path as a tag name: each dash-separated word
// capitalized, the folders joined by sep ("" for <SectionsHero>, "." for
// <Sections.Hero>). It returns "" for a path with an empty word.
func pascal(path, sep string) string {
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		words := strings.Split(seg, "-")
		for j, w := range words {
			if w == "" {
				return ""
			}
			if w[0] >= 'a' && w[0] <= 'z' {
				w = string(w[0]-'a'+'A') + w[1:]
			}
			words[j] = w
		}
		segs[i] = strings.Join(words, "")
	}
	return strings.Join(segs, sep)
}

func (p *htmlPrinter) tagOpen(tok scanner.Token) {
	form, ok := p.form(tok.Name)
	if !ok {
		p.problem(tok.Pos, tok.Label()+" has a name "+Studio.Name()+" cannot write")
	}
	p.out.write("<" + form.open)

	end := ">"
	if tok.SelfClose {
		end = " />"
	}
	raw, lexed := p.off.tagSource(tok.Pos, "x-")
	if !lexed || len(raw.attrs) != len(tok.Attrs) {
		for _, a := range tok.Attrs {
			if a.Bound {
				if joined, ok := interpolated(a.Value); ok {
					a.Value, a.Bound = joined, false
				} else {
					a.Value = htmlscan.Friendly(a.Value)
				}
			}
			text, fits := plainAttr(a)
			if !fits {
				p.problem(a.Pos, `attribute "`+a.Name+`" uses both kinds of quotes, which one attribute cannot hold`)
				continue
			}
			p.out.write(" " + text)
		}
		p.out.write(end)
		return
	}
	for i, a := range tok.Attrs {
		ra := raw.attrs[i]
		p.out.write(ra.lead)
		if !a.Bound {
			p.out.write(ra.static())
			continue
		}
		if text, ok := interpolated(a.Value); ok {
			if q, fits := pickQuote(text, ra.quote); fits {
				p.out.write(a.Name + "=" + string(q) + text + string(q))
				continue
			}
		}
		value := htmlscan.Friendly(a.Value)
		q, fits := pickQuote(value, ra.quote)
		if !fits {
			p.problem(a.Pos, `attribute "`+a.Name+`" uses both kinds of quotes, which one attribute cannot hold`)
			continue
		}
		p.out.write(":" + a.Name + "=" + string(q) + value + string(q))
	}
	if tok.SelfClose {
		end = "/>"
	}
	p.out.write(raw.tail + end)
}

// interpolated writes a bound attribute's joined text the way an .html author
// would, with the values dropped into the text:
//
//	'Hi ' . $name . '!'   →   Hi {{ name }}!
//	'' . $id              →   {{ id }}
//
// Only an expression that joins at least one piece of plain text qualifies, and
// only when the HTML scanner reads the result back as the very expression it
// came from — so 'Hi '.$name (unspaced), $a . $b (no text) and a text holding
// {{ all stay as they are, bound.
func interpolated(expr string) (string, bool) {
	operands := joinOperands(expr)
	if len(operands) < 2 {
		return "", false
	}
	var b strings.Builder
	texts := 0
	for _, op := range operands {
		if op != "" && op[0] == '\'' {
			text, ok := stringLiteral(op)
			if !ok {
				return "", false
			}
			texts++
			b.WriteString(text)
			continue
		}
		if len(op) > 1 && op[0] == '(' && closes(op) {
			op = op[1 : len(op)-1]
		}
		b.WriteString("{{ " + htmlscan.Friendly(op) + " }}")
	}
	if texts == 0 {
		return "", false
	}
	text := b.String()
	if back, ok := htmlscan.Interpolate(text); !ok || back != expr {
		return "", false
	}
	return text, true
}

// joinOperands splits an expression at each " . " that stands outside a string
// and outside any brackets. It is a rough cut: interpolated checks the result.
func joinOperands(expr string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(expr); i++ {
		switch c := expr[i]; {
		case c == '\'' || c == '"':
			for i++; i < len(expr) && expr[i] != c; i++ {
				if expr[i] == '\\' {
					i++
				}
			}
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && strings.HasPrefix(expr[i:], " . "):
			out = append(out, expr[start:i])
			i += 2
			start = i + 1
		}
	}
	return append(out, expr[start:])
}

// closes reports whether the parenthesis s opens with is the one it ends on,
// so that (a ?? b) is one group and (a) . (b) is not.
func closes(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'' || c == '"':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i == len(s)-1
			}
		}
	}
	return false
}
