// Package parser turns the scanner's flat token stream into an ast tree.
//
// It is a recursive-descent parser over []scanner.Token with an explicit stack
// of open constructs ("frames"). Nesting is depth-matched: a closer (@endif,
// @endforeach, </x-name>, </x-slot>) is bound to the innermost matching open
// construct, so same-kind constructs nest correctly — the failure a regex
// engine cannot avoid and the reason this AST exists.
//
// The parser never panics on arbitrary input. Every structural error is
// recoverable: it records a Diag and keeps parsing, preferring to surface the
// user's content over discarding it.
//   - Unclosed construct at EOF: Diag at the OPENING token, body kept as if
//     closed at EOF.
//   - Stray closer (no matching open construct): Diag, token dropped.
//   - Malformed @foreach args: Diag, the loop node dropped, body still parsed
//     and surfaced where the loop would have been.
//
// Only the standard library is imported.
package parser

import (
	"regexp"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// Diag is a recoverable parse error. It mirrors the scanner's position model
// (1-based line/col in bytes). Construct names the specific construct the
// error is about — "@if", "@foreach", "@endif", a component tag ("<x-hero>"),
// a slot ("<x-slot:head>") — so downstream diagnostics can say more than a
// generic "parse"; it is "" only if a diag site has nothing more specific.
type Diag struct {
	File      string
	Line      int
	Col       int
	Construct string
	Message   string
}

// Parse builds the AST for tokens, which must come from scanner.Scan(file, …).
// It returns the top-level nodes and any diagnostics; the node slice is always
// usable (best-effort) even when diags is non-empty.
func Parse(file string, tokens []scanner.Token) ([]ast.Node, []Diag) {
	p := &parser{file: file, tokens: tokens}
	nodes := p.parseBody(bodyCtx{})
	return nodes, p.diags
}

type parser struct {
	file   string
	tokens []scanner.Token
	pos    int
	frames []frame
	diags  []Diag
}

// bodyCtx threads context into a body parse. inForeach gates @break/@continue;
// component is non-nil only when the body is the DIRECT body of a component,
// which is the sole place a named <x-slot:…> is legal.
type bodyCtx struct {
	inForeach bool
	component *ast.Component
}

// frameKind identifies which construct an open frame represents.
type frameKind int

const (
	fIf frameKind = iota
	fForeach
	fComponent
	fSlot
)

// frame is one open construct on the stack. name is the component name (used
// only by fComponent to match its </x-name>).
type frame struct {
	kind frameKind
	name string
}

// accepts reports whether tok is the closer (or, for fIf, a continuation) of
// this open construct.
func (f frame) accepts(tok scanner.Token) bool {
	switch f.kind {
	case fIf:
		return tok.Kind == scanner.Directive &&
			(tok.Name == "endif" || tok.Name == "elseif" || tok.Name == "else")
	case fForeach:
		return tok.Kind == scanner.Directive && tok.Name == "endforeach"
	case fComponent:
		return tok.Kind == scanner.TagClose && tok.Name == f.name
	case fSlot:
		return tok.Kind == scanner.SlotClose
	}
	return false
}

func (p *parser) eof() bool { return p.pos >= len(p.tokens) }

func (p *parser) peek() scanner.Token { return p.tokens[p.pos] }

func (p *parser) next() scanner.Token {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *parser) push(f frame) { p.frames = append(p.frames, f) }

func (p *parser) pop() { p.frames = p.frames[:len(p.frames)-1] }

func (p *parser) diag(pos scanner.Pos, construct, msg string) {
	p.diags = append(p.diags, Diag{File: pos.File, Line: pos.Line, Col: pos.Col, Construct: construct, Message: msg})
}

// isBoundary reports whether tok closes/continues the current construct OR any
// enclosing one. When it belongs to an ancestor, the current construct is
// unclosed and control must unwind to that ancestor.
func (p *parser) isBoundary(tok scanner.Token) bool {
	for i := len(p.frames) - 1; i >= 0; i-- {
		if p.frames[i].accepts(tok) {
			return true
		}
	}
	return false
}

// parseBody parses nodes until a boundary token (a closer for this or an
// enclosing construct) or EOF. It never consumes the boundary token — the
// caller inspects it to decide whether its own construct closed normally.
func (p *parser) parseBody(ctx bodyCtx) []ast.Node {
	var nodes []ast.Node
	for !p.eof() {
		if p.isBoundary(p.peek()) {
			break
		}
		nodes = append(nodes, p.parseNode(ctx)...)
	}
	return nodes
}

// parseNode consumes at least one token and returns the node(s) it produced
// (nil for tokens that carry no node — comments, strays). Consuming ≥1 token on
// every path is what guarantees the parser always terminates.
func (p *parser) parseNode(ctx bodyCtx) []ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case scanner.Text:
		p.next()
		return one(&ast.Text{Bytes: tok.Text, P: tok.Pos})
	case scanner.Echo:
		p.next()
		return one(&ast.Echo{Expr: tok.Text, Raw: false, P: tok.Pos})
	case scanner.RawEcho:
		p.next()
		return one(&ast.Echo{Expr: tok.Text, Raw: true, P: tok.Pos})
	case scanner.Comment:
		p.next() // comments carry no AST node
		return nil
	case scanner.Directive:
		return p.parseDirective(ctx)
	case scanner.TagOpen:
		return one(p.parseComponent(ctx))
	case scanner.TagClose:
		p.diag(tok.Pos, "<x-"+tok.Name+">", "stray </x-"+tok.Name+"> with no open component")
		p.next()
		return nil
	case scanner.SlotOpen:
		return p.parseSlot(ctx)
	case scanner.SlotClose:
		p.diag(tok.Pos, "<x-slot>", "stray </x-slot> outside a component body")
		p.next()
		return nil
	default:
		p.next() // defensive: never spin on an unexpected kind
		return nil
	}
}

func (p *parser) parseDirective(ctx bodyCtx) []ast.Node {
	tok := p.peek()
	switch tok.Name {
	case "if":
		return one(p.parseIf(ctx))
	case "foreach":
		return p.parseForeach(ctx)
	case "break", "continue":
		p.next()
		if !ctx.inForeach {
			p.diag(tok.Pos, "@"+tok.Name, "@"+tok.Name+" outside a @foreach")
			return nil
		}
		return one(&ast.LoopJump{Cond: tok.Text, Continue: tok.Name == "continue", P: tok.Pos})
	case "props":
		p.next()
		return one(&ast.Props{Raw: tok.Text, P: tok.Pos})
	case "vite":
		p.next()
		return one(&ast.Vite{Raw: tok.Text, P: tok.Pos})
	case "endif", "elseif", "else", "endforeach":
		// Reaching here means no open construct wants this closer.
		p.next()
		p.diag(tok.Pos, "@"+tok.Name, "stray @"+tok.Name+" with no matching open construct")
		return nil
	default:
		// Unknown directive: the renderer writes Src verbatim and classifies it
		// app-tier.
		p.next()
		return one(&ast.Unknown{Name: tok.Name, Raw: tok.Text, Src: tok.Src, P: tok.Pos})
	}
}

// parseIf handles @if … [@elseif …]* [@else …] @endif.
func (p *parser) parseIf(ctx bodyCtx) ast.Node {
	open := p.next() // @if
	node := &ast.If{P: open.Pos}
	p.push(frame{kind: fIf})
	defer p.pop()

	node.Branches = append(node.Branches, ast.Branch{
		Cond: open.Text,
		P:    open.Pos,
		Body: p.parseBody(branchCtx(ctx)),
	})

	inElse := false
	for {
		if p.eof() {
			p.diag(open.Pos, "@if", "unclosed @if")
			return node
		}
		tok := p.peek()
		if tok.Kind == scanner.Directive {
			switch tok.Name {
			case "elseif":
				if inElse {
					break // @elseif after @else — treat @if as unclosed here
				}
				p.next()
				node.Branches = append(node.Branches, ast.Branch{
					Cond: tok.Text,
					P:    tok.Pos,
					Body: p.parseBody(branchCtx(ctx)),
				})
				continue
			case "else":
				if inElse {
					break // second @else — treat @if as unclosed here
				}
				p.next()
				inElse = true
				node.Else = p.parseBody(branchCtx(ctx))
				continue
			case "endif":
				p.next()
				return node
			}
		}
		// An ancestor's closer (or a stray @else/@elseif we won't accept):
		// this @if is unclosed; unwind without consuming.
		p.diag(open.Pos, "@if", "unclosed @if")
		return node
	}
}

// parseForeach handles @foreach (args) … @endforeach. On malformed args the
// loop node is dropped but the body is still parsed and returned in its place.
func (p *parser) parseForeach(ctx bodyCtx) []ast.Node {
	open := p.next() // @foreach
	node := &ast.Foreach{P: open.Pos}
	argsOK := p.parseForeachArgs(open, node)

	p.push(frame{kind: fForeach})
	body := p.parseBody(bodyCtx{inForeach: true})
	p.pop()

	if !p.eof() && p.peek().Kind == scanner.Directive && p.peek().Name == "endforeach" {
		p.next()
	} else {
		p.diag(open.Pos, "@foreach", "unclosed @foreach")
	}

	if !argsOK {
		return body // loop dropped; its body surfaces where the loop would have been
	}
	node.Body = body
	return one(node)
}

// parseComponent handles <x-name …> … </x-name> and the self-closing form.
func (p *parser) parseComponent(ctx bodyCtx) ast.Node {
	open := p.next() // TagOpen
	comp := &ast.Component{
		Name:      open.Name,
		Attrs:     open.Attrs,
		Slots:     map[string][]ast.Node{},
		SelfClose: open.SelfClose,
		P:         open.Pos,
	}
	if open.SelfClose {
		return comp
	}

	p.push(frame{kind: fComponent, name: open.Name})
	comp.Body = p.parseBody(bodyCtx{inForeach: ctx.inForeach, component: comp})
	p.pop()

	if !p.eof() && p.peek().Kind == scanner.TagClose && p.peek().Name == open.Name {
		p.next()
	} else {
		p.diag(open.Pos, "<x-"+open.Name+">", "unclosed <x-"+open.Name+">")
	}
	return comp
}

// parseSlot handles a <x-slot:name>. It is a named slot only when directly
// inside a component body; otherwise it is stray — diagnosed and dropped, its
// contents left to parse as ordinary body nodes.
func (p *parser) parseSlot(ctx bodyCtx) []ast.Node {
	tok := p.peek()
	if ctx.component == nil {
		p.diag(tok.Pos, "<x-slot:"+tok.Name+">", "<x-slot:"+tok.Name+"> is only valid directly inside a component body")
		p.next()
		return nil
	}
	p.parseNamedSlot(ctx)
	return nil // slot content attaches to the component, not the body stream
}

func (p *parser) parseNamedSlot(ctx bodyCtx) {
	open := p.next() // SlotOpen
	name := open.Name

	var body []ast.Node
	if !open.SelfClose {
		p.push(frame{kind: fSlot})
		body = p.parseBody(bodyCtx{inForeach: ctx.inForeach})
		p.pop()
		if !p.eof() && p.peek().Kind == scanner.SlotClose {
			p.next()
		} else {
			p.diag(open.Pos, "<x-slot:"+name+">", "unclosed <x-slot:"+name+">")
		}
	}

	key := slotBindingName(name)
	if _, dup := ctx.component.Slots[key]; dup {
		p.diag(open.Pos, "<x-slot:"+name+">", "duplicate slot \""+name+"\"; last one wins")
	}
	ctx.component.Slots[key] = body // last one wins
}

// slotBindingName maps a named slot's source name to the variable name it binds
// in the component scope. Real Blade (ComponentTagCompiler::compileSlots)
// applies Str::camel to an INLINE slot name only when it contains a dash — so
// <x-slot:foo-bar> binds $fooBar and <x-slot:foo-bar_baz> binds $fooBarBaz
// (Str::camel splits on both '-' and '_'), but <x-slot:foo_bar> binds $foo_bar
// untouched. Oracle-probed 2026-07-18 (Laravel 12.64). Applied at parse time
// (the compile-time analogue) so duplicate detection and last-wins operate on
// the binding name, in document order, exactly as compiled Blade would.
func slotBindingName(name string) string {
	if !strings.Contains(name, "-") {
		return name
	}
	var b strings.Builder
	b.Grow(len(name))
	upperNext := false
	first := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' || c == '_' {
			upperNext = !first // only capitalize once a letter has been emitted
			continue
		}
		switch {
		case first:
			b.WriteByte(lowerByte(c))
			first = false
		case upperNext:
			b.WriteByte(upperByte(c))
			upperNext = false
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func upperByte(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

// branchCtx derives the context for an @if branch body: break/continue legality
// is inherited, but the branch is NOT directly inside a component, so a named
// slot within it is stray.
func branchCtx(ctx bodyCtx) bodyCtx {
	return bodyCtx{inForeach: ctx.inForeach}
}

var reLoopVar = regexp.MustCompile(`^\$[a-zA-Z_][a-zA-Z0-9_]*$`)

// parseForeachArgs fills node from the directive's argument text, splitting on
// the last top-level " as " and then on "=>". It returns false (with a Diag)
// when the grammar or variable names are invalid.
func (p *parser) parseForeachArgs(open scanner.Token, node *ast.Foreach) bool {
	left, right, found := splitLastAs(open.Text)
	if !found {
		p.diag(open.Pos, "@foreach", `malformed @foreach: expected "<expr> as $item"`)
		return false
	}
	items := strings.TrimSpace(left)
	if items == "" {
		p.diag(open.Pos, "@foreach", "malformed @foreach: empty loop expression")
		return false
	}

	keyRaw, valRaw, hasKey := splitArrow(right)
	if hasKey {
		key := strings.TrimSpace(keyRaw)
		val := strings.TrimSpace(valRaw)
		if !reLoopVar.MatchString(key) || !reLoopVar.MatchString(val) {
			p.diag(open.Pos, "@foreach", "malformed @foreach: invalid loop variable(s)")
			return false
		}
		node.KeyVar = key
		node.ValVar = val
	} else {
		val := strings.TrimSpace(right)
		if !reLoopVar.MatchString(val) {
			p.diag(open.Pos, "@foreach", "malformed @foreach: invalid loop variable")
			return false
		}
		node.ValVar = val
	}
	node.ItemsExpr = items
	return true
}

// splitLastAs splits s on the LAST top-level " as " (a literal space-a-s-space,
// case-sensitive, outside quotes and parentheses/brackets/braces). It returns
// the left/right substrings and whether a separator was found.
func splitLastAs(s string) (left, right string, found bool) {
	depth := 0
	var quote byte
	last := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++ // skip the escaped byte
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ' ':
			if depth == 0 && i+3 < len(s) && s[i+1] == 'a' && s[i+2] == 's' && s[i+3] == ' ' {
				last = i
			}
		}
	}
	if last < 0 {
		return "", "", false
	}
	return s[:last], s[last+4:], true
}

// splitArrow splits s on the first top-level "=>" (outside quotes and brackets).
// When absent it returns (s, "", false).
func splitArrow(s string) (key, val string, found bool) {
	depth := 0
	var quote byte
	for i := 0; i+1 < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 && s[i+1] == '>' {
				return s[:i], s[i+2:], true
			}
		}
	}
	return s, "", false
}

func one(n ast.Node) []ast.Node { return []ast.Node{n} }
