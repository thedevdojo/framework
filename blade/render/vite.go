package render

import (
	"strings"

	"github.com/thedevdojo/framework/blade/ast"
	"github.com/thedevdojo/framework/site"
)

// viteTailwindCDN is the Tailwind v4 Play CDN script src the static
// projection emits. Kept byte-identical to the PHP side's StaticVite::CDN
// constant (laravel/src/StaticVite.php) — the two must never drift.
const viteTailwindCDN = "https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"

// parseStringArray parses a @vite([...]) argument — a literal array of quoted
// strings, no keys — reusing @props's top-level splitter and string-literal
// parser (props.go): only the grammar differs (bare string entries only, no
// `=>` defaults). Anything outside this grammar returns ok=false, which the
// renderer surfaces as a @vite error diagnostic and renders nothing.
func parseStringArray(raw string) ([]string, bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, false
	}
	inner := s[1 : len(s)-1]
	parts, ok := splitTopLevel(inner, ',')
	if !ok {
		return nil, false
	}
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue // tolerate a trailing comma / empty array
		}
		lit, ok := parseStringLiteral(part)
		if !ok {
			return nil, false
		}
		out = append(out, lit)
	}
	return out, true
}

// parseViteEntries parses @vite's full argument grammar — wider than @props's
// (which stays array-only): the `['a', 'b']` array literal, the idiomatic
// bare-string form `@vite('a.css')` (real Blade accepts it, and the PHP side's
// StaticVite normalizes a bare string the same way — Go must match for engine
// parity), and the empty form (`@vite()` / bare `@vite` → no entries, valid;
// StaticVite's null→[] normalization renders nothing there too). Anything
// else returns ok=false.
func parseViteEntries(raw string) ([]string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, true // @vite / @vite(): no entries, nothing to project
	}
	if lit, ok := parseStringLiteral(s); ok {
		return []string{lit}, true // bare-string form: @vite('x.css')
	}
	return parseStringArray(s)
}

// renderVite handles a @vite([...]) directive as the static projection: the
// Tailwind Play CDN script once, plus one inlined
// <style type="text/tailwindcss"> block per resolved CSS entry, in argument
// order, joined by "\n" — the same emission StaticVite produces on the PHP
// side (see laravel/src/StaticVite.php and fixtures/vite-css, vite-bare).
//
// A .js entry is app-tier (real bundling, not the static projection) and is
// skipped; a missing CSS file is an error and its block is omitted; a
// malformed argument (neither a literal string array nor a bare string
// literal) is an error and nothing renders. Zero resolved CSS entries means
// zero output — including the CDN script, since there is then nothing for it
// to serve — matching StaticVite's empty-styles HtmlString(”).
func (r *renderer) renderVite(node *ast.Vite) {
	entries, ok := parseViteEntries(node.Raw)
	if !ok {
		r.diag(node.P, KindError, say(node.P, "@vite", "<Vite>"), say(node.P,
			"invalid @vite([...]) literal",
			`<Vite> needs a src, like <Vite src="src/css/site.css" />`))
		return
	}

	var styles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry, ".js") {
			r.diag(node.P, KindAppTier, say(node.P, "@vite-js", "<Vite>"),
				say(node.P, "@vite", "<Vite>")+" js entry \""+entry+"\" needs a real bundler; skipped")
			continue
		}
		if r.ctx == nil {
			continue // defensive: no filesystem to resolve against
		}
		// The entry path joins deps whether or not the asset exists: a missing
		// asset is a dependency on the path @vite looked for, so a
		// render-on-save host re-renders this output when the file is created.
		// A site moved from resources/ to src/ may still name its stylesheet in
		// the old folder: it is read from the folder the site uses, and the
		// author is told which line to change.
		named := entry
		entry, moved := r.ctx.siteLayout().Asset(named)
		r.ctx.deps.add(entry)
		src, ok := r.ctx.fs.Read(entry)
		if ok && moved && r.ctx.notices != nil {
			*r.ctx.notices = append(*r.ctx.notices, site.AssetNotice(node.P.File, named, entry))
		}
		if !ok {
			r.diag(node.P, KindError, say(node.P, "@vite", "<Vite>"), "vite asset \""+entry+"\" not found")
			continue
		}
		styles = append(styles, string(src))
	}

	if len(styles) == 0 {
		return
	}
	r.sb.WriteString(`<script src="` + viteTailwindCDN + `"></script>`)
	for _, css := range styles {
		r.sb.WriteString("\n" + `<style type="text/tailwindcss">` + css + `</style>`)
	}
}
