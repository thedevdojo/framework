package render

import (
	"strconv"
	"strings"

	"github.com/thedevdojo/framework/blade/ast"
	"github.com/thedevdojo/framework/blade/expr"
	"github.com/thedevdojo/framework/blade/parser"
	"github.com/thedevdojo/framework/blade/scanner"
	"github.com/thedevdojo/framework/site"
)

// componentMaxDepth caps component nesting. A component file rendered deeper than
// this is refused with a component-recursion diagnostic. Real Blade has no such
// cap (a self-including component loops forever), so the fixture for it is
// oracle:false; the number is a DevDojo Framework safety bound.
const componentMaxDepth = 16

// rawHTML is a pre-rendered slot value. Real Blade passes slot content as an
// HtmlString, so {{ $slot }} emits it without escaping — the echo path treats a
// value of this type as already-safe HTML.
type rawHTML string

// String lets a slot be joined into text by the evaluator's `.`, as PHP joins
// an HtmlString: by its contents.
func (h rawHTML) String() string { return string(h) }

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
	// (real Blade's View::share leaks $site into anonymous components).
	site expr.Value
	// shared holds the collection and content bindings, which since 2026-07-28
	// are ALSO global — a component or layout reads $links straight from
	// collections/links.json without the page threading it through (spec
	// docs/specs/2026-07-28-collections-global-scope-design.md). Options.Data
	// stays out of it: the protocol's data map is per-page view data and, like
	// real Blade's per-view data, never reaches a component's isolated scope.
	shared map[string]expr.Value
	// layout is where this site keeps its files (site.Detect, once per
	// Compile). A renderer built without Compile has the zero Layout; the
	// accessors below fall back to the default one.
	layout site.Layout
	// notices collects what a render learns that the site's author should be
	// told but that is not a problem (an asset named in the old folder). It is
	// shared by every entry of a Compile.
	notices *[]site.Notice
	// loopCap / loopUsed implement the per-entry @foreach iteration budget
	// (Options.MaxLoopIterations): every renderer sharing this ctx — the page,
	// its components, its slots — draws from the same allowance. exceeded
	// latches once the budget runs out so the abort diagnoses exactly once
	// and renderNodes stops walking at every level. A zero loopCap (a
	// renderer built without Compile, as some unit tests do) disables the
	// budget entirely.
	loopCap  int
	loopUsed int
	exceeded bool
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
	r.diag(node.P, KindError, "props", say(node.P,
		"@props is only valid as the first element of a component file",
		"<Props> is only valid as the first thing in a component file"))
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

	// Every path the resolver tries joins deps whether or not the file exists:
	// a missing component is a dependency on each path it LOOKED FOR, so a
	// render-on-save host diffing deps re-renders this output the moment the
	// user creates the file. Same normalized form as every resolved dep. A Blade
	// <x-name> names one file; a Studio-syntax tag may name several (see
	// componentFiles), and the first that exists wins.
	var path string
	var src []byte
	found := false
	for _, candidate := range componentFiles(r.ctx.componentsRoot(), node) {
		r.ctx.deps.add(candidate)
		if b, ok := r.ctx.fs.Read(candidate); ok {
			path, src, found = candidate, b, true
			break
		}
	}
	if !found {
		r.sb.WriteString(missingMarker(node.Name))
		r.diag(node.P, KindError, "component-missing", missingMessage(r.ctx.componentsRoot(), node))
		return
	}

	nodes, pdiags := r.parseFile(path, src)
	r.diags = append(r.diags, foldParseDiags(pdiags)...)

	// The leading @props (if any) declares this file's props and is the one
	// Props node that renders silently.
	propsNode := extractLegalProps(nodes)
	declared := propSet{}
	if propsNode != nil {
		if ps, issues, ok := parseProps(propsNode.Raw); ok {
			declared = ps
			// A valid-but-unmodelable default (nested/assoc array) leaves its
			// prop declared without a default; each one is app-tier — real
			// Blade accepts it, this engine cannot carry the value.
			for _, msg := range issues {
				r.diag(propsNode.P, KindAppTier, "props", say(propsNode.P, msg, strings.Replace(msg, "@props", "<Props>", 1)))
			}
		} else {
			r.diag(propsNode.P, KindError, "props", say(propsNode.P,
				"invalid @props(...) literal",
				`<Props> has a default that is not a plain value — use text, or a number, true, false, null or a list after a colon (:count="3")`))
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
	// Real Blade returns ltrim(ob_get_clean()) for every rendered view
	// (PhpEngine::evaluatePath), so a component's rendered output has its LEADING
	// whitespace stripped (default ltrim charset; trailing bytes untouched). A
	// component authored with a blank line after @props, or leading indentation,
	// therefore contributes no leading whitespace — matched here byte-for-byte.
	r.sb.WriteString(phpLtrim(child.sb.String()))
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
	tokens := scanSource(path, src)
	nodes, diags := parser.Parse(path, tokens)
	if r.ctx.cache != nil {
		r.ctx.cache[path] = nodes
	}
	return nodes, diags
}

// buildComponentScope produces the FLAT, isolated scope a component body sees,
// seeded low → high exactly as real Blade builds an anonymous component's scope:
//
//  1. the GLOBAL shared bindings (collections + content)
//  2. $site (shared last on the PHP side, so it outranks a collection named
//     `site`)
//  3. passed attributes — they arrive as the component view's PER-VIEW data,
//     and Factory::gatherData merges shared then view data, so a passed
//     attribute beats a same-named shared binding EVEN WHEN ITS VALUE IS NULL
//  4. @props DEFAULTS, but only where the name is still null — compileProps
//     emits `$x = $x ?? $default`, so a default can NOT displace a shared
//     collection, yet it DOES fill in behind a passed null
//  5. $slot and the named slots
//
// Steps 3–5 are oracle-verified (2026-07-28); see the correction section of
// docs/specs/2026-07-28-collections-global-scope-design.md and the
// component-props-vs-collection / component-props-null-vs-default fixtures.
//
// Page-LOCAL data — the protocol's Options.Data keys and any caller locals such
// as a loop variable — does NOT leak in. Bound attributes are evaluated in the
// caller's scope at the tag site; a bound-attr eval error is diagnosed and the
// prop becomes nil, and that nil is an error sentinel which (unlike a genuine
// passed null) does NOT fall through to the prop's default; the attribute is
// also left out of the bag so the breakage stays visible.
//
// $attributes (2026-08-13): every attribute the @props declaration does not
// claim — the claim set is each declared name plus its kebab form, real
// Blade's extractPropNames — lands in the component's $attributes bag, in tag
// order, mirroring compileProps. Values enter the bag SANITIZED the way the
// compiled tag calls BladeCompiler::sanitizeComponentAttribute: a bound
// attribute whose value is a string is e()-escaped; everything else passes
// through. A prop's camelCase variable still receives the RAW value (it comes
// from the anonymous component's data array, compiled with escapeBound=false),
// while a snake_case attribute's literal-name variable receives the SANITIZED
// value (it reaches the scope through the bag; view data merges the bag's
// original keys first). A valueless attribute (<x-btn wide/>) is bool TRUE in
// both props and bag, exactly as the tag compiler builds it.
func buildComponentScope(caller *renderer, declared propSet, attrs []scanner.Attr, slot rawHTML, named map[string]rawHTML) componentScope {
	// Resolve every passed attribute value, keyed by each name it binds (real
	// Blade matches foo-bar="…" to the fooBar prop, and logo_text="…" to BOTH
	// $logo_text and $logoText — see attrBindingNames).
	passed := make(map[string]expr.Value, len(attrs))
	// failed records attributes whose bound expression could not be evaluated.
	// Their nil is an ERROR SENTINEL, not the value null, so it must not feed the
	// `??` defaults pass below — a broken binding that quietly renders its
	// default would hide the very breakage the diagnostic is reporting.
	failed := map[string]bool{}
	// claimed is extractPropNames over the @props declaration: each declared
	// name plus its kebab form. Attributes under a claimed name are consumed
	// as props; the rest fill the $attributes bag.
	claimed := map[string]bool{}
	for name := range declared {
		claimed[name] = true
		claimed[kebabCase(name)] = true
	}
	var bagEntries []expr.AttrEntry
	for _, a := range attrs {
		keys := attrBindingNames(a.Name)
		var raw, sanitized expr.Value
		evalFailed := false
		switch {
		case a.Valueless:
			raw, sanitized = true, true
		case a.Bound:
			v, err := expr.Eval(a.Value, caller.scope)
			if err != nil {
				caller.diagExprError(err, a.Pos)
				v = nil
				evalFailed = true
				for _, key := range keys {
					failed[key] = true
				}
			}
			raw, sanitized = v, v
			// sanitizeComponentAttribute: a bound STRING value is e()-escaped on
			// its way into the attribute array; other types pass through.
			if s, ok := v.(string); ok {
				sanitized = Escape(s)
			}
		default:
			// Real Blade interpolates {{ }} / {!! !!} inside a STATIC attribute
			// value (heading="Hi {{ $name }}"); this engine passes the braces
			// through literally — a silent byte divergence on an otherwise clean
			// page. Flag it app-tier (the value still passes as written) so the
			// divergence is visible rather than hidden.
			if strings.Contains(a.Value, "{{") || strings.Contains(a.Value, "{!!") {
				caller.diag(a.Pos, KindAppTier, "attribute-interpolation",
					"attribute \""+a.Name+"\" interpolates {{ }} into a static value; requires the PHP app tier")
			}
			raw, sanitized = a.Value, a.Value // static attr: the verbatim string
		}
		camel := camelCase(a.Name)
		for _, key := range keys {
			if key == camel {
				// The camelCase binding is the anonymous component's data array,
				// compiled with escapeBound=false — the raw value.
				passed[key] = raw
			} else {
				// The literal (snake_case) binding reaches the scope through the
				// bag's original keys — the sanitized value.
				passed[key] = sanitized
			}
		}
		if !evalFailed && !claimed[a.Name] {
			bagEntries = append(bagEntries, expr.AttrEntry{Name: a.Name, Val: sanitized})
		}
	}

	cs := componentScope{}
	if caller.ctx != nil {
		// Collections and content are global: a component reads $links straight
		// from collections/links.json, no page threading required. Seeded FIRST
		// (lowest precedence) so $site and the props passes can override them.
		for name, v := range caller.ctx.shared {
			cs[name] = v
		}
		// $site is global too (parity with View::share leaking into anonymous
		// components), and outranks the shared collections: `site` is shared
		// LAST on the PHP side, so a collection literally named `site` can never
		// displace the site object.
		if caller.ctx.site != nil {
			cs["site"] = caller.ctx.site
		}
	}
	// EVERY passed attribute binds in the component scope — not just the
	// declared ones. Real Blade's anonymous-component view data carries ALL
	// tag attributes (partitionDataAndAttributes returns everything as data
	// for a classless component), camelized and raw, plus the bag's original
	// keys — so an undeclared foo_bar="…" still defines $fooBar (and
	// $foo_bar) inside the component. Oracle-verified 2026-08-13
	// (attribute-bag-escaping fixture: $logoText renders with only logo_text
	// declared). Passed attributes arrive as PER-VIEW data, which
	// Factory::gatherData merges OVER the shared data — so they overwrite a
	// same-named collection even when their value is null.
	for name, v := range passed {
		cs[name] = v
	}
	for name, decl := range declared {
		// The defaults pass, which compileProps emits as `$x = $x ?? $default`:
		// the default fills in wherever the name is still null — nothing shared
		// bound it, or a passed value was itself null. Hence
		// @props(['links' => null]) alongside a collections/links.json renders
		// the COLLECTION (a default cannot shadow shared data), while
		// <x-nav :links="null"/> renders the DEFAULT (the null overwrote the
		// collection first, then the default filled the hole). Both verified
		// against the oracle, 2026-07-28. An attribute whose expression FAILED to
		// evaluate is excluded: real Blade would have raised rather than reached
		// this line, so there is no parity to model, and leaving the prop nil
		// keeps the breakage visible next to its diagnostic.
		if decl.hasDefault && cs[name] == nil && !failed[name] {
			cs[name] = decl.def
		}
		// bare prop, not passed → whatever shared data bound (often nothing,
		// leaving the variable undefined)
	}
	// $attributes is always bound in a component scope (an empty bag when every
	// attribute was consumed by @props). Set after the props passes — the
	// compiled view data merges ['attributes' => $bag] last — and before the
	// slots, whose names the factory injects on top.
	cs["attributes"] = expr.NewAttrBag(bagEntries)
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

// componentsRoot is where this site's component files live, in both syntaxes
// (src/components/ unless the site uses one of the older shapes).
func (c *renderCtx) componentsRoot() string {
	return c.siteLayout().Components
}

// siteLayout is the layout this render reads from: the one Compile detected,
// or the default for a renderer built without Compile (as some unit tests do).
func (c *renderCtx) siteLayout() site.Layout {
	if c == nil || c.layout.Root == "" {
		return site.Default()
	}
	return c.layout
}

// componentPath maps a dotted component name to its exact source file:
// sections.hero → src/components/sections/hero.blade.php.
func componentPath(componentsRoot, name string) string {
	return componentsRoot + strings.ReplaceAll(name, ".", "/") + ".blade.php"
}

// componentFiles lists the files a component tag may name, in the order to try
// them.
//
// A Blade <x-sections.hero> names exactly one: its .blade.php, as in real
// Laravel. It deliberately never falls back to an .html component — a Blade
// page must keep rendering the same bytes real Blade would.
//
// A Studio-syntax tag carries its candidates (see htmlscan.Candidates): each is
// tried as .html and then as .blade.php, so an .html page can use a component
// from an existing Blade library unchanged.
func componentFiles(componentsRoot string, node *ast.Component) []string {
	if len(node.Candidates) == 0 {
		return []string{componentPath(componentsRoot, node.Name)}
	}
	out := make([]string, 0, 2*len(node.Candidates))
	for _, base := range node.Candidates {
		out = append(out, componentsRoot+base+".html", componentsRoot+base+".blade.php")
	}
	return out
}

// missingMessageMaxPaths caps how many looked-for paths a missing-component
// message spells out; a long tag name can have dozens.
const missingMessageMaxPaths = 4

// missingMessage is the diagnostic text for a component no file was found for.
// For a Studio-syntax tag it lists where the engine looked, because the tag
// alone (<SectionsHero>) does not say which file name it expects.
func missingMessage(componentsRoot string, node *ast.Component) string {
	if node.Tag == "" {
		return "component \"" + node.Name + "\" not found"
	}
	var looked []string
	for i, base := range node.Candidates {
		if i == missingMessageMaxPaths {
			looked = append(looked, "and "+strconv.Itoa(len(node.Candidates)-i)+" more")
			break
		}
		looked = append(looked, componentsRoot+base+".html")
	}
	return "component <" + node.Tag + "> not found — looked for " + strings.Join(looked, ", ")
}

// missingMarker is the byte-stable output marker for an unresolved component.
func missingMarker(name string) string {
	return `<!-- devdojo:missing component "` + name + `" -->`
}

// attrBindingNames returns every scope name a passed attribute binds, in
// precedence order (later wins).
//
// Real Blade binds an attribute under its camelCased name — foo-bar="…" reaches
// the fooBar prop — AND, when the name written on the tag is itself a usable PHP
// variable name, under that literal name too. So logo_text="Hi" fills BOTH
// $logo_text and $logoText, while logo-text="Hi" fills only $logoText ($logo-text
// is not a variable). Oracle-verified 2026-08-07.
//
// Keying by the camelCase name alone silently dropped every snake_case
// attribute: @props(['logo_text' => 'Default']) kept rendering its default no
// matter what the tag passed.
func attrBindingNames(name string) []string {
	camel := camelCase(name)
	if name != camel && isPHPVariableName(name) {
		return []string{name, camel}
	}
	return []string{camel}
}

// isPHPVariableName reports whether s can appear after a `$` — letters, digits
// and underscores, not opening with a digit. It is what separates logo_text
// (a variable real Blade binds) from logo-text (which only binds camelized).
func isPHPVariableName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
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

// kebabCase mirrors Laravel's Str::kebab (Str::snake with '-'): a '-' lands
// before every non-leading uppercase letter, then everything lowercases. So
// logoText → logo-text, FooBar → foo-bar, and an already-lowercase name
// (logo_text) is unchanged. It is how extractPropNames derives the second
// claimed form of each declared prop.
func kebabCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteByte(lower(c))
			continue
		}
		b.WriteByte(c)
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

// isPhpTrim reports whether b is in PHP's default trim/ltrim character mask:
// " \t\n\r\0\x0B" (space, tab, newline, carriage return, null, vertical tab).
// Note: form feed (0x0C) is NOT in the set — a leading form feed survives, as
// verified against the oracle.
func isPhpTrim(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', 0, '\v':
		return true
	}
	return false
}

// phpTrim strips leading and trailing bytes the way PHP's trim() does (the
// isPhpTrim mask). Real Blade trims slot contents with trim(), so slot
// whitespace matches byte-for-byte.
func phpTrim(s string) string {
	i := 0
	for i < len(s) && isPhpTrim(s[i]) {
		i++
	}
	j := len(s)
	for j > i && isPhpTrim(s[j-1]) {
		j--
	}
	return s[i:j]
}

// phpLtrim strips only LEADING bytes the way PHP's ltrim() does with its default
// mask (isPhpTrim). Real Blade's PhpEngine::evaluatePath returns
// ltrim(ob_get_clean()) for EVERY rendered view, so both page and component
// output shed their leading whitespace while trailing bytes are preserved.
func phpLtrim(s string) string {
	i := 0
	for i < len(s) && isPhpTrim(s[i]) {
		i++
	}
	return s[i:]
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
