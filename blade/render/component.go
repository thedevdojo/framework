package render

import (
	"strconv"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/blade/parser"
	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// componentMaxDepth caps component nesting. A component file rendered deeper than
// this is refused with a component-recursion diagnostic. Real Blade has no such
// cap (a self-including component loops forever), so the fixture for it is
// oracle:false; the number is a Pocketknife safety bound.
const componentMaxDepth = 16

// rawHTML is a pre-rendered slot value. Real Blade passes slot content as an
// HtmlString, so {{ $slot }} emits it without escaping — the echo path treats a
// value of this type as already-safe HTML.
type rawHTML string

// renderCtx is the render state shared across an entire Compile: the source
// filesystem, the deps set of the OUTPUT currently rendering, and a per-Compile
// parse cache. Every component render (however deeply nested, and every slot
// rendered in a caller scope) shares one ctx so deps and the cache accumulate in
// one place.
type renderCtx struct {
	fs    vfsReader
	deps  *orderedSet
	cache map[string][]ast.Node
	// site is the global $site object. It is shared into every component scope
	// (real Blade's View::share leaks $site into anonymous components), unlike
	// collections/content/Options.Data, which pages see but components do not.
	site expr.Value
}

// vfsReader is the slice of vfs.FS the renderer needs (only Read). Declared here
// as an interface so component.go does not import vfs directly.
type vfsReader interface {
	Read(path string) ([]byte, bool)
}

// propDecl is one declared prop: its default value and whether it has one. A
// bare @props entry (['bare']) declares a name with hasDefault=false — when the
// attribute is not passed the variable is left UNDEFINED (real Blade behavior).
type propDecl struct {
	hasDefault bool
	def        expr.Value
}

// propSet maps a declared prop name (as written in @props, camelCase) to its
// declaration.
type propSet map[string]propDecl

// renderProps handles a @props node encountered during the walk. The single
// legal declaration (recognized during scope setup) renders as nothing; any
// other @props — not first in a component file, or anywhere on a page — is a
// props error and is ignored.
func (r *renderer) renderProps(node *ast.Props) {
	if node == r.legalProps {
		return
	}
	r.diag(node.P, KindError, "props", "@props is only valid as the first element of a component file")
}

// renderComponent resolves, loads, and renders an <x-name …/> tag, splicing the
// result into the current output. Missing components emit the byte-stable marker
// and a diagnostic; the recursion cap refuses runaway nesting. It never panics.
func (r *renderer) renderComponent(node *ast.Component) {
	// Rendering this component's FILE would sit at depth+1; refuse if that would
	// exceed the cap. Tracked per render-stack (r.depth), so it unwinds cleanly.
	if r.depth+1 > componentMaxDepth {
		r.diag(node.P, KindError, "component-recursion", "component nesting exceeded "+strconv.Itoa(componentMaxDepth)+" levels")
		return
	}
	if r.ctx == nil {
		return // defensive: a renderer with no ctx cannot resolve components
	}

	path := componentPath(node.Name)
	src, ok := r.ctx.fs.Read(path)
	if !ok {
		r.sb.WriteString(missingMarker(node.Name))
		r.diag(node.P, KindError, "component-missing", "component \""+node.Name+"\" not found")
		return
	}
	r.ctx.deps.add(path)

	nodes, pdiags := r.parseFile(path, src)
	r.diags = append(r.diags, foldParseDiags(pdiags)...)

	// The leading @props (if any) declares this file's props and is the one
	// Props node that renders silently.
	propsNode := extractLegalProps(nodes)
	declared := propSet{}
	if propsNode != nil {
		if ps, ok := parseProps(propsNode.Raw); ok {
			declared = ps
		} else {
			r.diag(propsNode.P, KindError, "props", "invalid @props(...) literal")
		}
	}

	// Slots render in the CALLER's scope (this renderer's scope + depth), so they
	// see caller locals — including an enclosing loop variable — and their nested
	// components add to deps too. Real Blade PHP-trims slot content.
	slot := rawHTML(phpTrim(r.renderChild(node.Body, r.scope, r.depth)))
	named := map[string]rawHTML{}
	for name, body := range node.Slots {
		named[name] = rawHTML(phpTrim(r.renderChild(body, r.scope, r.depth)))
	}

	cs := buildComponentScope(r, declared, node.Attrs, slot, named)

	// The component file renders at depth+1 in its own isolated scope, with its
	// leading @props marked legal so it stays silent.
	child := &renderer{scope: cs, ctx: r.ctx, depth: r.depth + 1, legalProps: propsNode}
	child.renderNodes(nodes, false)
	r.sb.WriteString(child.sb.String())
	r.diags = append(r.diags, child.diags...)
}

// renderChild renders a body to a string in the given scope at the given depth,
// sharing this renderer's ctx (so deps/cache accumulate) and folding its
// diagnostics back in. Used for slot bodies, which are caller-scope content.
func (r *renderer) renderChild(nodes []ast.Node, scope expr.Scope, depth int) string {
	if len(nodes) == 0 {
		return ""
	}
	sub := &renderer{scope: scope, ctx: r.ctx, depth: depth}
	sub.renderNodes(nodes, false)
	r.diags = append(r.diags, sub.diags...)
	return sub.sb.String()
}

// parseFile scans+parses a component file, caching the node tree per path for
// this Compile. Parse diagnostics are returned only on a cache MISS — a cache
// hit means this path was already scanned+parsed (and its diags already
// folded in by the caller) earlier in this same Compile, so returning nil
// avoids re-diagnosing a file rendered from several call sites. (Even without
// this, dedupeDiagnostics would collapse the repeats at Result assembly — see
// diagnostics.go — but skipping the redundant work here is simpler and
// cheaper.)
func (r *renderer) parseFile(path string, src []byte) ([]ast.Node, []parser.Diag) {
	if r.ctx.cache != nil {
		if nodes, ok := r.ctx.cache[path]; ok {
			return nodes, nil
		}
	}
	tokens := scanner.Scan(path, src)
	nodes, diags := parser.Parse(path, tokens)
	if r.ctx.cache != nil {
		r.ctx.cache[path] = nodes
	}
	return nodes, diags
}

// buildComponentScope produces the FLAT, isolated scope a component body sees:
// only its declared props (defaults, overridden by passed attributes) plus $slot
// and the named slots. Page/caller data does NOT leak in. Bound attributes are
// evaluated in the caller's scope at the tag site; a bound-attr eval error is
// diagnosed (Task 6 mapping) and the prop becomes nil. Undeclared attributes are
// dropped silently.
func buildComponentScope(caller *renderer, declared propSet, attrs []scanner.Attr, slot rawHTML, named map[string]rawHTML) componentScope {
	// Resolve every passed attribute value, keyed by its camelCased name (real
	// Blade matches foo-bar="…" to the fooBar prop).
	passed := make(map[string]expr.Value, len(attrs))
	for _, a := range attrs {
		key := camelCase(a.Name)
		if a.Bound {
			v, err := expr.Eval(a.Value, caller.scope)
			if err != nil {
				caller.diagExprError(err, a.Pos)
				v = nil
			}
			passed[key] = v
		} else {
			// Real Blade interpolates {{ }} / {!! !!} inside a STATIC attribute
			// value (heading="Hi {{ $name }}"); this engine passes the braces
			// through literally — a silent byte divergence on an otherwise clean
			// page. Flag it app-tier (the value still passes as written) so the
			// divergence is visible rather than hidden.
			if strings.Contains(a.Value, "{{") || strings.Contains(a.Value, "{!!") {
				caller.diag(a.Pos, KindAppTier, "attribute-interpolation",
					"attribute \""+a.Name+"\" interpolates {{ }} into a static value; requires the PHP app tier")
			}
			passed[key] = a.Value // static attr: the verbatim string
		}
	}

	cs := componentScope{}
	// $site is global: a component sees it (parity with View::share leaking into
	// anonymous components). Declared props of the same name still win — set site
	// first so a passed/defaulted `site` prop overrides it, matching PHP where a
	// component's own data overrides shared data.
	if caller.ctx != nil && caller.ctx.site != nil {
		cs["site"] = caller.ctx.site
	}
	for name, decl := range declared {
		if v, ok := passed[name]; ok {
			cs[name] = v
		} else if decl.hasDefault {
			cs[name] = decl.def
		}
		// bare prop, not passed → left undefined (absent from the scope)
	}
	cs["slot"] = slot
	for name, val := range named {
		cs[name] = val
	}
	return cs
}

// componentScope is a component body's flat variable scope. It has no parent:
// isolation from caller/page data is the whole point.
type componentScope map[string]expr.Value

func (s componentScope) Lookup(name string) (expr.Value, bool) {
	v, ok := s[name]
	return v, ok
}

// extractLegalProps returns the one @props node that legally declares a
// component file's props: the first non-(whitespace-Text) node, when it is a
// Props. Leading whitespace-only Text nodes are skipped. Returns nil when the
// first meaningful node is not a @props (any @props elsewhere is then illegal).
func extractLegalProps(nodes []ast.Node) *ast.Props {
	for _, n := range nodes {
		if t, ok := n.(*ast.Text); ok && isAllWhitespace(t.Bytes) {
			continue
		}
		if p, ok := n.(*ast.Props); ok {
			return p
		}
		return nil
	}
	return nil
}

// componentPath maps a dotted component name to its exact source file:
// sections.hero → resources/views/components/sections/hero.blade.html.
func componentPath(name string) string {
	return "resources/views/components/" + strings.ReplaceAll(name, ".", "/") + ".blade.html"
}

// missingMarker is the byte-stable output marker for an unresolved component.
func missingMarker(name string) string {
	return `<!-- pocketknife:missing component "` + name + `" -->`
}

// camelCase mirrors Laravel's Str::camel: split on '-'/'_' word boundaries,
// upper-case the first letter of each following word, lower-case the very first
// letter. It is what maps a kebab-case attribute (foo-bar) to a camelCase prop
// (fooBar).
func camelCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	upperNext := false
	first := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' || c == '_' {
			upperNext = !first // only capitalize if we've emitted a letter already
			continue
		}
		switch {
		case first:
			b.WriteByte(lower(c))
			first = false
		case upperNext:
			b.WriteByte(upper(c))
			upperNext = false
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

// isAllWhitespace reports whether s is empty or only ASCII whitespace.
func isAllWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isSpaceByte(s[i]) {
			return false
		}
	}
	return true
}

func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// phpTrim strips leading and trailing bytes the way PHP's trim() does: the set
// " \t\n\r\0\x0B" (note: NOT form feed). Real Blade trims slot contents with
// trim(), so slot whitespace matches byte-for-byte.
func phpTrim(s string) string {
	isTrim := func(b byte) bool {
		switch b {
		case ' ', '\t', '\n', '\r', 0, '\v':
			return true
		}
		return false
	}
	i := 0
	for i < len(s) && isTrim(s[i]) {
		i++
	}
	j := len(s)
	for j > i && isTrim(s[j-1]) {
		j--
	}
	return s[i:j]
}

// orderedSet is an insertion-ordered, deduplicated string set — used for deps so
// the entry stays first and each component file appears once, in encounter
// order.
type orderedSet struct {
	seen  map[string]struct{}
	items []string
}

func newOrderedSet() *orderedSet {
	return &orderedSet{seen: map[string]struct{}{}}
}

func (o *orderedSet) add(s string) {
	if _, ok := o.seen[s]; ok {
		return
	}
	o.seen[s] = struct{}{}
	o.items = append(o.items, s)
}

func (o *orderedSet) list() []string {
	out := make([]string, len(o.items))
	copy(out, o.items)
	return out
}
