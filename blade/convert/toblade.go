package convert

import (
	"strings"

	"github.com/thedevdojo/framework/blade/htmlscan"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/blade/scanner"
)

// printBlade writes a Studio-syntax file's tokens as Blade.
//
// The tokens are already Blade's (that is what the HTML scanner emits), so the
// tags are the easy half. The care goes into the text between them. An .html
// file has no directives, so "@click", "@media" and "<x-" are plain text
// there; in Blade each would be read as something. Every such spot is defused
// so that the text prints byte for byte the same — and the yardstick is real
// Laravel, not only this engine: the reason to convert to Blade is to run the
// site there. Where the two differ the comments below say so.
//
// tidy first turns each hatch — a character the Studio syntax printed through a
// raw echo, {!! '<' !!}!-- — back into text. The text printer hatches it again
// where Blade needs that too, and writes it plainly where Blade does not. File
// falls back to the untidied print if the tidy one does not read back the same.
func printBlade(path string, src []byte, toks []scanner.Token, exists func(string) bool, tidy bool) (string, []render.Diagnostic) {
	p := &bladePrinter{path: path, off: newOffsets(src), exists: exists}
	if tidy {
		toks = unhatch(toks)
	}
	for i, tok := range toks {
		switch tok.Kind {
		case scanner.Text:
			p.text(tok.Text)
		case scanner.Echo:
			p.brace("{{ " + tok.Text + " }}")
		case scanner.RawEcho:
			p.brace("{!! " + tok.Text + " !!}")
		case scanner.Comment:
			p.brace(p.off.commentSource(tok.Pos))
		case scanner.Directive:
			p.directive(toks, i)
		case scanner.TagOpen:
			p.tagOpen(tok)
		case scanner.TagClose:
			p.plain("</x-" + p.componentName(tok) + ">")
		case scanner.SlotOpen:
			p.plain("<x-slot:" + tok.Name + ">")
		case scanner.SlotClose:
			p.plain("</x-slot>")
		case scanner.Error:
			// A tag the HTML scanner could read but not act on (<Layout> with
			// no src). It already says what to write instead.
			p.problem(tok.Pos, tok.Text)
		}
	}
	return p.out.String(), p.problems
}

// unhatch returns toks with every hatch turned into the text it prints, joined
// to the text around it so that what the two spell together is seen whole.
func unhatch(toks []scanner.Token) []scanner.Token {
	out := make([]scanner.Token, 0, len(toks))
	for _, tok := range toks {
		if tok.Kind == scanner.RawEcho {
			if c, ok := hatched(tok.Text); ok {
				tok = scanner.Token{Kind: scanner.Text, Text: string(c), Pos: tok.Pos}
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

type bladePrinter struct {
	path   string
	off    offsets
	exists func(string) bool
	out    buffer
	// bare is true right after a directive written without arguments (@else,
	// @endif). What follows must not read as its continuation.
	bare     bool
	problems []render.Diagnostic
}

func (p *bladePrinter) problem(pos scanner.Pos, msg string) {
	p.problems = append(p.problems, problem(p.path, pos.Line, pos.Col, msg))
}

func (p *bladePrinter) plain(s string) {
	p.bare = false
	p.out.write(s)
}

// brace writes a construct that opens with a brace. A '{' before it would open
// it one brace early, and an '@' before it would switch it off — "@{{ x }}"
// and, in Laravel, "@{!! x !!}" are both escapes — so any such bytes at the
// end of the output are reprinted through hatches first.
func (p *bladePrinter) brace(s string) {
	p.bare = false
	p.out.hatchTrailing("{@")
	p.out.write(s)
}

// hatch prints one character of text through a raw echo. It is a brace
// construct, so it gets the same care.
func (p *bladePrinter) hatch(c byte) {
	p.out.hatchTrailing("@")
	p.out.write(hatch(c))
}

// dir writes a directive. Blade only sees one where the byte before the '@' is
// not part of a word (text@endif is text), and "@@" is its escape, so a word
// byte gets a separator after it and a trailing '@' is hatched.
func (p *bladePrinter) dir(s string, bare bool) {
	switch last := p.out.last(); {
	case last == '@':
		p.out.hatchTrailing("@")
	case isIdentPart(last):
		p.out.write(separator)
	}
	p.out.write(s)
	p.bare = bare
}

// text writes a run of plain text.
func (p *bladePrinter) text(s string) {
	if p.bare {
		p.bare = false
		// @else followed by "more" is @elsemore; by "::x" Laravel reads a
		// namespaced directive; by "(" — after any whitespace, line breaks
		// included, for this engine — the parenthesis becomes its arguments.
		// The separator goes directly before the offending byte, so a line
		// break the directive swallows is still the first thing after it.
		i := 0
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		switch {
		case s != "" && isIdentPart(s[0]):
			p.out.write(separator)
		case len(s) > 2 && s[0] == ':' && s[1] == ':' && isIdentPart(s[2]):
			p.out.write(separator)
		case i < len(s) && s[i] == '(':
			p.out.write(s[:i] + separator)
			s = s[i:]
		}
	}
	p.literal(s)
}

// literal writes text so that Blade reads every byte of it as text.
func (p *bladePrinter) literal(s string) {
	for i := 0; i < len(s); {
		rest := s[i:]
		switch s[i] {
		case '@':
			boundary := !isIdentPart(p.out.last())
			switch {
			case strings.HasPrefix(rest, "@{{"):
				// A real '@' before real braces. Printed through a hatch it
				// cannot be taken for the escape the braces get next.
				p.hatch('@')
			case boundary && len(rest) > 2 && rest[1] == '@' && isIdentPart(rest[2]):
				// "@@word" is Blade's escape and would print one '@' fewer.
				p.hatch('@')
			case boundary && len(rest) > 1 && isIdentStart(rest[1]):
				// "@word": escape it as "@@word". Laravel drops the spaces
				// between an escaped directive and a parenthesis after it
				// ("@@media (…)" prints "@media(…)"), so there the '@' is
				// hatched instead and nothing is left to compile.
				j := 1
				for j < len(rest) && isIdentPart(rest[j]) {
					j++
				}
				k := j
				for k < len(rest) && (rest[k] == ' ' || rest[k] == '\t') {
					k++
				}
				if k > j && k < len(rest) && rest[k] == '(' {
					p.hatch('@')
				} else {
					p.out.write("@@")
				}
			default:
				p.out.write("@")
			}
			i++

		case '{':
			switch {
			case strings.HasPrefix(rest, "{{--"), strings.HasPrefix(rest, "{!!"):
				p.hatch('{')
				i++
			case strings.HasPrefix(rest, "{{"):
				// Literal braces take Blade's own escape, "@{{ … }}", when what
				// is between them is tame. Laravel compiles directives, tags
				// and comments before it looks at echoes, so anything of that
				// kind inside would be compiled anyway; then the first brace
				// is hatched and the rest is written piece by piece.
				end := strings.Index(rest[2:], "}}")
				if end < 0 || strings.ContainsAny(rest[2:2+end], "@{<") {
					p.hatch('{')
					i++
					continue
				}
				p.out.hatchTrailing("@")
				p.out.write("@" + rest[:2+end+2])
				i += 2 + end + 2
			default:
				p.out.write("{")
				i++
			}

		case '<':
			// Laravel reads "<x-", "<x:" and their closing forms as component
			// tags, with spaces allowed after the '<'; and a Blade file is a
			// PHP file, so "<?" would open PHP (an <?xml ?> line, say).
			j := 1
			if j < len(rest) && rest[j] == '/' {
				j++
			}
			for j < len(rest) && isSpace(rest[j]) {
				j++
			}
			if strings.HasPrefix(rest, "<?") || (j+1 < len(rest) && rest[j] == 'x' && (rest[j+1] == '-' || rest[j+1] == ':')) {
				p.hatch('<')
			} else {
				p.out.write("<")
			}
			i++

		default:
			p.out.write(s[i : i+1])
			i++
		}
	}
}

func (p *bladePrinter) directive(toks []scanner.Token, i int) {
	tok := toks[i]
	switch tok.Name {
	case "if", "elseif":
		p.dir("@"+tok.Name+" ("+tok.Text+")", false)
	case "else", "endif", "endforeach":
		p.dir("@"+tok.Name, true)
	case "foreach":
		p.dir("@foreach ("+tok.Text+")", false)
		if limit, ok := loopLimit(tok); ok {
			p.checkLimit(toks, i, limit)
			// Blade has no limit. "Stop once this many have been shown" is
			// the same loop as long as nothing inside asks how long the list
			// is — which checkLimit has just made sure of.
			if i+1 < len(toks) && toks[i+1].Kind == scanner.Text && strings.HasPrefix(toks[i+1].Text, "\n") {
				p.out.write("\n")
			}
			p.dir("@break("+limitCondition(limit)+")", false)
		}
	case "break", "continue", "vite":
		if tok.Text == "" {
			p.dir("@"+tok.Name, true)
		} else {
			p.dir("@"+tok.Name+"("+tok.Text+")", false)
		}
	case "props":
		p.props(tok)
	case "class", "style":
		if strings.TrimSpace(tok.Text) == "" {
			// No list at all. Bare, Laravel compiles it to class="" as this
			// engine does; written @class() it calls the helper with nothing
			// and the page fails.
			p.dir("@"+tok.Name, true)
			return
		}
		p.dir("@"+tok.Name+"("+tok.Text+")", false)
	}
}

// props writes <Props> as @props, one entry per line when the tag was written
// that way.
func (p *bladePrinter) props(tok scanner.Token) {
	text := tok.Text
	if props, ok := propList(text); ok {
		entries := make([]string, len(props))
		for i, pr := range props {
			entries[i] = phpString(pr.name) + " => " + bladeDefault(pr.value)
		}
		text = "[" + strings.Join(entries, ", ") + "]"
		if raw, ok := p.off.tagSource(tok.Pos, ""); ok && strings.Contains(raw.text, "\n") && len(entries) > 0 {
			indent := p.off.indent(tok.Pos)
			text = "[\n" + indent + "    " + strings.Join(entries, ",\n"+indent+"    ") + ",\n" + indent + "]"
		}
	}
	p.dir("@props("+text+")", false)
}

// bladeDefault writes a prop's default the way a person would in PHP: text
// with an apostrophe in it goes in double quotes ("There's more") rather than
// behind a backslash — as long as PHP reads those double quotes as plain text
// too, which a '$' or a backslash would change.
func bladeDefault(value string) string {
	if s, ok := stringLiteral(value); ok && value[0] == '\'' && strings.Contains(s, "'") && !strings.ContainsAny(s, "\"$\\") {
		return `"` + s + `"`
	}
	return value
}

// checkLimit refuses a <Foreach limit> that @foreach + @break would not
// reproduce. A limit cuts the list before the loop starts; @break stops a loop
// over the whole list. They differ in exactly two ways: what loop.last and
// loop.count say, and when the limit itself is worked out (once outside the
// loop, or on every pass inside it).
func (p *bladePrinter) checkLimit(toks []scanner.Token, i int, limit string) {
	if why := limitRefusal(toks, i, limit); why != "" {
		p.problem(toks[i].Pos, toks[i].Label()+" "+why+", so it cannot be converted")
	}
}

// limitRefusal says why the loop at toks[i] cannot trade a limit for a leading
// @break (or the @break for a limit), or "" when the two are the same loop.
func limitRefusal(toks []scanner.Token, i int, limit string) string {
	_, key, val, _ := loopParts(toks[i].Text)
	for _, name := range variables(limit) {
		if name == "loop" || "$"+name == val || (key != "" && "$"+name == key) {
			return "has a limit that reads " + name + ", the loop's own item; Blade would work it out differently"
		}
	}
	depth := 0
	for j := i + 1; j < len(toks); j++ {
		t := toks[j]
		if t.Kind == scanner.Directive && t.Name == "endforeach" {
			if depth == 0 {
				return ""
			}
			depth--
			continue
		}
		if depth == 0 {
			exprs := []string{}
			switch t.Kind {
			case scanner.Echo, scanner.RawEcho, scanner.Directive:
				exprs = append(exprs, t.Text)
			}
			for _, a := range t.Attrs {
				if a.Bound || t.Kind == scanner.Directive {
					exprs = append(exprs, a.Value)
				}
			}
			for _, e := range exprs {
				if member, bad := loopLength(e); bad {
					return "has a limit and uses " + member + " inside; Blade has no limit, and its @break would leave " + member + " describing the whole list"
				}
			}
		}
		if t.Kind == scanner.Directive && t.Name == "foreach" {
			depth++
		}
	}
	return ""
}

// variables lists the $names a Blade expression reads, outside its strings.
func variables(e string) []string {
	var out []string
	for i := 0; i < len(e); i++ {
		switch c := e[i]; {
		case c == '\'' || c == '"':
			for i++; i < len(e) && e[i] != c; i++ {
				if e[i] == '\\' {
					i++
				}
			}
		case c == '$':
			j := i + 1
			for j < len(e) && isIdentPart(e[j]) {
				j++
			}
			if j > i+1 {
				out = append(out, e[i+1:j])
			}
			i = j - 1
		}
	}
	return out
}

// loopLength reports whether a Blade expression asks $loop how long the list
// is — $loop->last, $loop->count — or hands the whole $loop on to something
// that might. It returns the offending use, spelled the HTML way.
func loopLength(e string) (string, bool) {
	for i := 0; i < len(e); i++ {
		switch c := e[i]; {
		case c == '\'' || c == '"':
			for i++; i < len(e) && e[i] != c; i++ {
				if e[i] == '\\' {
					i++
				}
			}
		case strings.HasPrefix(e[i:], "$loop") && (i+5 == len(e) || !isIdentPart(e[i+5])):
			rest := e[i+5:]
			if !strings.HasPrefix(rest, "->") {
				return "loop", true
			}
			j := 2
			for j < len(rest) && isIdentPart(rest[j]) {
				j++
			}
			switch member := rest[2:j]; member {
			case "index", "iteration", "first", "odd", "even":
			default:
				return "loop." + member, true
			}
			i += 4
		}
	}
	return "", false
}

// componentName is the Blade name of the component a Studio-syntax tag means:
// the first of its candidate files that exists, as the renderer picks it.
func (p *bladePrinter) componentName(tok scanner.Token) string {
	cands := tok.Candidates
	if tok.Kind == scanner.TagClose {
		// A closing tag carries no candidates. <Layout>'s and <Include>'s
		// already know the name their opening tag resolved; a component
		// tag's are worked out from its spelling, as its opening tag's were.
		lower := strings.ToLower(tok.Tag)
		if lower == "layout" || lower == "include" {
			return tok.Name
		}
		cands = htmlscan.Candidates(tok.Tag)
	}
	return resolve(cands, p.exists)
}

func (p *bladePrinter) tagOpen(tok scanner.Token) {
	name := p.componentName(tok)
	for i := 0; i < len(name); i++ {
		if !isTagNameChar(name[i]) {
			p.problem(tok.Pos, tok.Label()+` names the file "`+strings.ReplaceAll(name, ".", "/")+`", which a Blade tag cannot spell`)
			break
		}
	}
	// A dot in a folder or file name would read as a folder in Blade.
	for _, c := range tok.Candidates {
		if dotted(c) == name && strings.Contains(c, ".") {
			p.problem(tok.Pos, tok.Label()+` names the file "`+c+`"; Blade reads a dot in a component name as a folder, so it cannot be converted`)
		}
	}
	p.plain("<x-" + name)

	end := ">"
	if tok.SelfClose {
		end = " />"
	}
	raw, lexed := p.off.tagSource(tok.Pos, "")
	if lexed {
		// <Layout src> and <Include src> spend their first src on naming the
		// file; it is not among the token's attributes.
		if lower := strings.ToLower(tok.Tag); lower == "layout" || lower == "include" {
			for i, ra := range raw.attrs {
				if ra.name == "src" {
					raw.attrs = append(raw.attrs[:i:i], raw.attrs[i+1:]...)
					break
				}
			}
		}
	}
	if !lexed || len(raw.attrs) != len(tok.Attrs) {
		for _, a := range tok.Attrs {
			text, fits := plainAttr(a)
			if !fits {
				p.problem(a.Pos, `attribute "`+a.Name+`" uses both kinds of quotes, which one Blade attribute cannot hold`)
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
			if ra.eq && a.Value != ra.value {
				// The value held an escaped brace (\{), which Blade writes
				// as the brace itself.
				q, fits := pickQuote(a.Value, ra.quote)
				if !fits {
					p.problem(a.Pos, `attribute "`+a.Name+`" uses both kinds of quotes, which one Blade attribute cannot hold`)
					continue
				}
				p.out.write(ra.name + "=" + string(q) + a.Value + string(q))
				continue
			}
			p.out.write(ra.static())
			continue
		}
		// Bound: either written with a colon, or text with {{ }} in it, which
		// is a bound attribute joining its pieces. Blade can only say the
		// second the first way.
		preferred := byte('"')
		if strings.HasPrefix(ra.name, ":") {
			preferred = ra.quote
		}
		q, fits := pickQuote(a.Value, preferred)
		if !fits {
			p.problem(a.Pos, `attribute "`+a.Name+`" uses both kinds of quotes, which one Blade attribute cannot hold`)
			continue
		}
		p.out.write(":" + a.Name + "=" + string(q) + a.Value + string(q))
	}
	if tok.SelfClose {
		end = "/>"
	}
	p.out.write(raw.tail + end)
}
