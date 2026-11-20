package render

import (
	"strings"

	"github.com/thedevdojo/framework/blade/ast"
	"github.com/thedevdojo/framework/blade/parser"
	"github.com/thedevdojo/framework/blade/scanner"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

// Analyze is the lint-only analysis pass, separate from Compile by design. It
// walks each entry and every component it transitively references, and returns
// the WARNING-kind diagnostics that Compile deliberately never emits — today,
// undeclared component attributes (spec §3.3: "Lint warns on undeclared
// attributes to catch typos").
//
// Why a separate pass rather than a flag on Compile: the compile protocol's
// diagnostic stream is consumed by hosts that treat ANY diagnostic as a hard
// failure (the devdojo Sites template-sync gate rejects a template on a single
// diagnostic of any kind). A warning must never reach that stream. Keeping the
// analysis out of Compile entirely — only `devdojo lint` calls Analyze —
// guarantees those any-diagnostic-fails semantics stay exactly as they were.
//
// Warnings are deduped by (file, line, col, message) so a component file
// referenced from several pages reports each internal instance once. Analyze
// never panics on user input.
func Analyze(fs vfs.FS, entries []string) []Diagnostic {
	a := &analyzer{fs: fs, components: site.Detect(fs).Components, walked: map[string]bool{}, cache: map[string][]ast.Node{}}
	for _, entry := range entries {
		if escapesRoot(entry) {
			continue
		}
		src, ok := fs.Read(entry)
		if !ok {
			continue
		}
		a.walk(a.parse(entry, src))
	}
	return dedupeDiagnostics(a.warnings)
}

// analyzer holds the lint walk state: the source tree, the collected warnings,
// the set of component files already descended into (so a file's internal
// instances are analyzed once, however many pages reach it), and a parse cache.
type analyzer struct {
	fs         vfsReader
	components string // the site's components folder
	warnings   []Diagnostic
	walked     map[string]bool
	cache      map[string][]ast.Node
}

// parse scans+parses a file, caching the node tree per path. Parse diagnostics
// are ignored here — Compile owns reporting those; Analyze only adds warnings.
func (a *analyzer) parse(path string, src []byte) []ast.Node {
	if nodes, ok := a.cache[path]; ok {
		return nodes
	}
	tokens, info := scanSource(path, src, nil)
	nodes, _ := parser.Parse(path, tokens)
	a.cache[path] = nodes
	// A single brace around what looks like a value the engine cannot read
	// ({route('home')}) is text, as it must be in a file that holds CSS and
	// JavaScript; say so, since the author most likely meant a value.
	for _, p := range info.NearMisses {
		a.warnings = append(a.warnings, Diagnostic{
			File:    p.File,
			Line:    p.Line,
			Col:     p.Col,
			Kind:    KindWarning,
			Message: "this brace prints as text: what is inside is not a value the HTML syntax can read (no function calls). Write \\{ if the brace is meant as text",
		})
	}
	return nodes
}

// walk descends a node list, checking every component instance and recursing
// into control-flow bodies, the instance's default slot, and its named slots.
func (a *analyzer) walk(nodes []ast.Node) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *ast.Component:
			a.checkInstance(v)
			a.walk(v.Body)
			for _, body := range v.Slots {
				a.walk(body)
			}
		case *ast.If:
			for _, br := range v.Branches {
				a.walk(br.Body)
			}
			a.walk(v.Else)
		case *ast.Foreach:
			a.walk(v.Body)
		}
	}
}

// checkInstance resolves a component instance to its file, compares the
// instance's STATIC attributes against the component's declared @props, and
// warns for each attribute that isn't declared. It then descends into the
// component file's own body exactly once (memoized in a.walked) so nested
// instances inside components are analyzed without re-warning per referrer.
func (a *analyzer) checkInstance(c *ast.Component) {
	// Resolve the instance the way the renderer does: the first file that
	// exists wins. A Studio-syntax tag may match several (<SectionsHero> is
	// sections/hero or sections-hero); the rest are collected so the author
	// hears which one is actually used.
	var path string
	var src []byte
	var others []string
	for _, candidate := range componentFiles(a.components, c) {
		b, ok := a.fs.Read(candidate)
		if !ok {
			continue
		}
		if path == "" {
			path, src = candidate, b
		} else {
			others = append(others, candidate)
		}
	}
	if path == "" {
		// Missing component is Compile's error, not a lint warning: the analyzer
		// cannot know the declared prop set, so it stays silent.
		return
	}
	// tag is the instance as its file spells it, for the messages below.
	tag := "<x-" + c.Name + ">"
	if c.Tag != "" {
		tag = "<" + c.Tag + ">"
	}
	if len(others) > 0 {
		a.warnings = append(a.warnings, Diagnostic{
			File:    c.P.File,
			Line:    c.P.Line,
			Col:     c.P.Col,
			Kind:    KindWarning,
			Message: tag + " matches more than one file — using " + path + ", not " + strings.Join(others, ", "),
		})
	}
	nodes := a.parse(path, src)

	declared, known := declaredProps(nodes)
	// The component file's own syntax decides how its props are declared and
	// how it reaches its attributes bag.
	bag, where := "$attributes", "(not in @props)"
	if isHTMLFile(path) {
		bag, where = "attributes", "(not in its <Props>)"
		// A Studio-syntax component may skip <Props> altogether — every passed
		// attribute becomes a variable regardless — so with no declaration
		// there is nothing to check an attribute against.
		if extractLegalProps(nodes) == nil {
			known = false
		}
	}
	// A component that reads $attributes accepts arbitrary extra attributes by
	// design (undeclared ones fill the bag), so warning on them would flag the
	// exact usage the feature exists for. A plain byte scan is deliberate: the
	// bag can be reached from any expression position (echo, @if, a bound
	// attribute), and a false "mention" merely silences a lint hint.
	if known && strings.Contains(string(src), bag) {
		known = false
	}
	if known {
		// Names bound as `:name="expr"` on this instance are the legitimate data
		// pass; a static attr of the same name is a fallback, not a typo.
		bound := boundAttrNames(c.Attrs)
		for _, at := range c.Attrs {
			if at.Bound {
				continue // only static attributes are analyzed
			}
			// An attribute is declared/bound under ANY of the names it binds:
			// logo_text="…" reaches both a logo_text and a logoText prop.
			if anyKnown(attrBindingNames(at.Name), declared, bound) {
				continue
			}
			a.warnings = append(a.warnings, Diagnostic{
				File:    at.Pos.File,
				Line:    at.Pos.Line,
				Col:     at.Pos.Col,
				Kind:    KindWarning,
				Message: `undeclared attribute "` + at.Name + `" on ` + tag + ` ` + where,
			})
		}
	}

	if !a.walked[path] {
		a.walked[path] = true
		a.walk(nodes)
	}
}

// declaredProps returns the component's declared prop set and whether it is
// KNOWN. A component with no @props has a known, empty set (every attribute is
// undeclared). A component whose @props literal cannot be parsed is UNKNOWN
// (known=false): the analyzer suppresses warnings rather than guess, leaving
// the props error for Compile to report on its own stream.
func declaredProps(nodes []ast.Node) (propSet, bool) {
	propsNode := extractLegalProps(nodes)
	if propsNode == nil {
		return propSet{}, true
	}
	ps, _, ok := parseProps(propsNode.Raw)
	if !ok {
		return nil, false
	}
	return ps, true
}

// boundAttrNames collects every name an instance's bound (`:name="expr"`)
// attributes bind — the camelCased name, plus the literal one where it is a
// usable variable name (see attrBindingNames).
func boundAttrNames(attrs []scanner.Attr) map[string]struct{} {
	out := map[string]struct{}{}
	for _, at := range attrs {
		if !at.Bound {
			continue
		}
		for _, name := range attrBindingNames(at.Name) {
			out[name] = struct{}{}
		}
	}
	return out
}

// anyKnown reports whether any of names appears in the declared prop set or the
// bound-attribute set.
func anyKnown(names []string, declared propSet, bound map[string]struct{}) bool {
	for _, name := range names {
		if _, ok := declared[name]; ok {
			return true
		}
		if _, ok := bound[name]; ok {
			return true
		}
	}
	return false
}
