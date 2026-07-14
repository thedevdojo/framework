// Package ast defines the syntax tree the parser produces and the renderer
// compiles against. Every node carries a scanner.Pos (file/line/col of its
// opening token) so diagnostics and render errors can point back at source.
//
// The node set is deliberately small and closed — it mirrors exactly the Blade
// subset Pocketknife supports. Directives the parser does not structurally
// understand become Unknown nodes; the renderer classifies those app-tier.
package ast

import "github.com/thedevdojo/pocketknife/blade/scanner"

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
type Foreach struct {
	ItemsExpr string
	KeyVar    string
	ValVar    string
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
// default-slot content; Slots holds named <x-slot:name> content.
type Component struct {
	Name      string
	Attrs     []scanner.Attr
	Body      []Node
	Slots     map[string][]Node
	SelfClose bool
	P         scanner.Pos
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

// Unknown is any directive the parser does not structurally handle (e.g.
// @auth, @php, @csrf). Name is the lowercased directive name, Raw its args.
// The renderer classifies these app-tier.
type Unknown struct {
	Name string
	Raw  string
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
func (n *Unknown) Pos() scanner.Pos   { return n.P }
