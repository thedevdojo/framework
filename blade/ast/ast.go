// Package ast defines the syntax tree the parser produces and the renderer
// compiles against. Every node carries a scanner.Pos (file/line/col of its
// opening token) so diagnostics and render errors can point back at source.
//
// The node set is deliberately small and closed — it mirrors exactly the Blade
// subset DevDojo Framework supports. Directives the parser does not structurally
// understand become Unknown nodes; the renderer classifies those app-tier.
package ast

import "github.com/thedevdojo/framework/blade/scanner"

// Node is any element of the tree. Pos reports the source position of the
// node's first byte.
type Node interface {
	Pos() scanner.Pos
}

// Text is an opaque run of source bytes emitted verbatim.
type Text struct {
	Bytes string
	P     scanner.Pos
}

// Echo is {{ expr }} (Raw=false) or {!! expr !!} (Raw=true).
type Echo struct {
	Expr string
	Raw  bool
	P    scanner.Pos
}

// If is an @if/@elseif/@else/@endif construct. Branches[0] is the @if; each
// subsequent entry is an @elseif. Else holds the optional @else body.
type If struct {
	Branches []Branch
	Else     []Node
	P        scanner.Pos
}

// Branch is one condition + body of an If (the @if or an @elseif).
type Branch struct {
	Cond string
	Body []Node
	P    scanner.Pos
}

// Foreach is @foreach (ItemsExpr as [KeyVar =>] ValVar) ... @endforeach.
// KeyVar is "" when the loop has no key binding.
//
// Limit is the Studio syntax's <Foreach limit="3">: an expression for how many
// items to loop over at most, "" for no limit. The loop runs over the list cut
// to that length, so $loop->count and $loop->last describe what is shown. Blade
// has no such argument, so a Blade @foreach never sets it.
type Foreach struct {
	ItemsExpr string
	KeyVar    string
	ValVar    string
	Limit     string
	Body      []Node
	P         scanner.Pos
}

// LoopJump is @break or @continue (Continue reports which). Cond holds the
// directive's argument expression; "" means an unconditional jump.
type LoopJump struct {
	Cond     string
	Continue bool
	P        scanner.Pos
}

// Component is an <x-name ...>...</x-name> (or self-closing) tag. Body is the
// default-slot content; Slots holds named <x-slot:name> content, keyed by the
// BINDING name (a source name containing '-' is camelized by the parser, the
// way real Blade's ComponentTagCompiler applies Str::camel to inline slot
// names), so <x-slot:foo-bar> appears here as "fooBar".
//
// Tag and Candidates are set only for a component written in the Studio syntax
// (<SectionsHero>, <Layout src>, <Include src>): Tag is the tag as written,
// and Candidates the component files it may name — paths relative to
// the components folder (src/components) without an extension, in the order to try them.
// Name is then the dotted name of the first candidate. A Blade <x-name> names
// exactly one file, derived from Name, and leaves both empty.
type Component struct {
	Name       string
	Tag        string
	Candidates []string
	Attrs      []scanner.Attr
	Body       []Node
	Slots      map[string][]Node
	SelfClose  bool
	P          scanner.Pos
}

// Props is an @props(...) directive; Raw is the literal argument text. Position
// legality (first non-Text node of a component file) is enforced at render.
type Props struct {
	Raw string
	P   scanner.Pos
}

// Vite is an @vite([...]) directive; Raw is the literal argument text.
type Vite struct {
	Raw string
	P   scanner.Pos
}

// ClassDir is a @class([...]) directive; Raw is the literal argument
// expression (an array literal, evaluated at render).
type ClassDir struct {
	Raw string
	P   scanner.Pos
}

// StyleDir is a @style([...]) directive; Raw is the literal argument
// expression.
type StyleDir struct {
	Raw string
	P   scanner.Pos
}

// Unknown is any directive the parser does not structurally handle (e.g.
// @auth, @php, @csrf). Name is the lowercased directive name, Raw its args, and
// Src the byte-faithful original source slice ('@' through the last consumed
// byte, verbatim casing/whitespace). The renderer writes Src verbatim (so an
// unregistered directive survives byte-for-byte, matching real Blade) and
// classifies the node app-tier.
type Unknown struct {
	Name string
	Raw  string
	Src  string
	P    scanner.Pos
}

func (n *Text) Pos() scanner.Pos      { return n.P }
func (n *Echo) Pos() scanner.Pos      { return n.P }
func (n *If) Pos() scanner.Pos        { return n.P }
func (n *Foreach) Pos() scanner.Pos   { return n.P }
func (n *LoopJump) Pos() scanner.Pos  { return n.P }
func (n *Component) Pos() scanner.Pos { return n.P }
func (n *Props) Pos() scanner.Pos     { return n.P }
func (n *Vite) Pos() scanner.Pos      { return n.P }
func (n *ClassDir) Pos() scanner.Pos  { return n.P }
func (n *StyleDir) Pos() scanner.Pos  { return n.P }
func (n *Unknown) Pos() scanner.Pos   { return n.P }
