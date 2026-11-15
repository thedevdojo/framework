// Package render compiles the Blade-subset AST into rendered bytes. It is the
// point where Pocketknife's Go pipeline (scanner → parser → expr) meets real
// Blade's output contract: echoes are escaped with Laravel's e(), values are
// stringified the PHP-echo way, and the newline swallowed by a compiled `?>` is
// reproduced exactly — a rule settled empirically against the oracle, NOT from
// the brief's hypothesis (see .superpowers/sdd/task-6-report.md).
//
// Task 6 scope: Text, Echo (escaped + raw), and If are rendered; comments carry
// no node (the parser drops them). Foreach/LoopJump: task 7. Component/Props:
// task 8/9. Vite: task 10 (see vite.go). Diagnostics — app-tier classification
// for Unknown/$attributes, parse-diag folding, entry-missing, and dedupe —
// are task 11 (see diagnostics.go). Compile never panics on arbitrary input.
package render

import (
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/blade/parser"
	"github.com/thedevdojo/pocketknife/blade/scanner"
	"github.com/thedevdojo/pocketknife/vfs"
)

// DiagKind classifies a diagnostic. KindError marks something the engine cannot
// render (undefined variable, unrenderable expression); KindAppTier marks valid
// Blade that needs real PHP and so falls to the app tier; KindWarning marks a
// non-fatal lint hint (an undeclared component attribute).
//
// KindAppTier is BLADE-ONLY. An .html (Studio-syntax) file cannot be handed to
// real Laravel, so it has no app tier: the same constructs are KindError there.
//
// KindWarning is LINT-ONLY: it is produced solely by Analyze (surfaced through
// `pocketknife lint`), never by Compile. The compile protocol's diagnostic
// stream carries only error/app-tier so that hosts which treat "any diagnostic"
// as a hard failure (e.g. the devdojo Sites template-sync validation gate) are
// untouched by warnings. See Analyze and the protocol package doc.
type DiagKind string

const (
	KindError   DiagKind = "error"
	KindAppTier DiagKind = "app-tier"
	KindWarning DiagKind = "warning"
)

// Diagnostic is a single problem found while rendering an entry. Line/Col are
// 1-based (bytes) and point at the construct's opening position.
type Diagnostic struct {
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Col       int      `json:"col"`
	Kind      DiagKind `json:"kind"`
	Construct string   `json:"construct,omitempty"`
	Message   string   `json:"message"`
}

// DefaultMaxLoopIterations is the per-entry @foreach iteration budget applied
// when Options.MaxLoopIterations is unset. It is a safety bound against
// pathological nesting over host data (real Blade has none — an unbounded
// nested loop simply runs), sized so no legitimate site comes near it: the
// whole fixture + template corpus renders in a few hundred iterations.
const DefaultMaxLoopIterations = 100_000

// Options carries render inputs. Data is the protocol's "data" map, merged over
// the site's data sources ($site, collections, content); its keys become
// top-level $variables and win on collision (a `site` key replaces $site).
type Options struct {
	Data map[string]any
	// MaxLoopIterations caps the TOTAL number of @foreach iterations one
	// entry's render may run, summed across nesting, components, and slots.
	// Exceeding it aborts that entry's render with a single
	// error/render-budget diagnostic (output truncated at the budget). Zero or
	// negative means DefaultMaxLoopIterations; there is no unlimited setting.
	MaxLoopIterations int
}

// Result is the output of a Compile: rendered files by output name, the source
// files each output depended on, and any diagnostics gathered along the way.
// Deps include paths that were LOOKED FOR but absent (a missing component's
// candidate file, a missing @vite asset) — a render-on-save host diffing a
// saved path against deps therefore re-renders when the missing file is
// created, not only when an existing dep changes.
type Result struct {
	Files       map[string]string   // output name → rendered bytes
	Deps        map[string][]string // output name → source files used or looked for
	Diagnostics []Diagnostic
}

// Newline-eating truth, settled empirically against the oracle (Step 3 probe;
// full log in .superpowers/sdd/task-6-report.md). PHP swallows one newline
// immediately after a compiled `?>`. Blade compiles block directives to
// `<?php … ?>` (so they eat the following newline) but appends a compensating
// newline after echoes and strips {{-- comments --}} before compilation (so
// neither of those eats). This is the OPPOSITE of the brief's echo hypothesis.
const (
	echoEatsNewline      = false
	directiveEatsNewline = true
)

// Variant is one render of an entry: the page at Entry, with Data merged over
// Options.Data for this render only, written to Output (the entry's usual
// output name when empty). A host renders a Folio-style dynamic page — one
// template, one document per collection entry — as a list of variants that
// differ only in the entry bound over the collection's name, so the site's
// sources and data are loaded and parsed once for the whole list rather than
// once per document.
type Variant struct {
	Entry  string
	Data   map[string]any
	Output string
}

// Compile renders each entry through the scanner → parser → renderer pipeline
// and returns the collected files, deps, and diagnostics. Missing entries are
// skipped. Compile never panics.
func Compile(fs vfs.FS, entries []string, opts Options) Result {
	variants := make([]Variant, len(entries))
	for i, entry := range entries {
		variants[i] = Variant{Entry: entry}
	}
	return CompileVariants(fs, variants, opts)
}

// CompileVariants renders each variant — an entry plus the data bound for that
// one render — over ONE load of the site's data sources and ONE parse cache.
// Two variants may share an entry as long as their outputs differ; a later
// variant writing the same output as an earlier one replaces it. Missing
// entries are skipped. CompileVariants never panics.
func CompileVariants(fs vfs.FS, variants []Variant, opts Options) Result {
	res := Result{
		Files: map[string]string{},
		Deps:  map[string][]string{},
	}
	// Assemble the site's data context once: $site, collections, and content
	// collections, with Options.Data merged over the top. Its diagnostics are
	// about the data files, not any one entry, so they attach once here.
	data := buildSiteData(fs, opts.Data)
	res.Diagnostics = append(res.Diagnostics, data.diags...)
	// parseCache is shared across every entry AND every component render in this
	// Compile: a component file resolved more than once (siblings, recursion) is
	// scanned+parsed only once.
	parseCache := map[string][]ast.Node{}
	loopCap := opts.MaxLoopIterations
	if loopCap <= 0 {
		loopCap = DefaultMaxLoopIterations
	}
	// pageOf remembers which entry produced each DEFAULT output name, to catch
	// two different pages landing on one file (pages/index.blade.php next to
	// pages/index.html). An explicit variant Output is the host's own choice
	// and is never a collision.
	pageOf := map[string]string{}
	for _, v := range variants {
		entry := v.Entry
		out := v.Output
		if out == "" {
			out = outputName(entry)
			if first, taken := pageOf[out]; taken && first != entry {
				res.Diagnostics = append(res.Diagnostics, pageCollisionDiagnostic(out, first, entry))
			} else if !taken {
				pageOf[out] = entry
			}
		}
		// Refuse an entry that escapes the site root — either the entry path
		// itself or the output name it would derive or was given (the
		// pages/views prefix strip can turn an in-root "../"-laden entry into
		// an escaping output name). Checked before any read/write so a crafted
		// entry can neither read outside the tree nor write above the
		// destination directory.
		if escapesRoot(entry) || escapesRoot(out) {
			res.Diagnostics = append(res.Diagnostics, entryInvalidDiagnostic(entry))
			continue
		}
		src, ok := fs.Read(entry)
		if !ok {
			res.Diagnostics = append(res.Diagnostics, entryMissingDiagnostic(entry))
			continue
		}
		tokens := scanSource(entry, src)
		nodes, pdiags := parser.Parse(entry, tokens)
		res.Diagnostics = append(res.Diagnostics, foldParseDiags(pdiags)...)
		// deps is per-output: it starts with the entry and gains every component
		// file and @vite asset the render resolves OR looks for and finds absent
		// (deduped, in encounter order, entry first).
		deps := newOrderedSet()
		deps.add(entry)
		// The loop budget is per entry: each output gets the full allowance, so
		// one pathological page cannot starve its siblings.
		ctx := &renderCtx{fs: fs, deps: deps, cache: parseCache, site: data.site, shared: data.shared, loopCap: loopCap}
		r := &renderer{scope: dataScope(variantBindings(data.bindings, v.Data)), ctx: ctx}
		r.renderNodes(nodes, false)
		// Real Blade returns ltrim(ob_get_clean()) for every rendered view
		// (PhpEngine::evaluatePath), so an entry page's leading whitespace is
		// stripped exactly as a component's is (see phpLtrim). Templates author
		// pages starting non-whitespace, so this is a no-op for them, but it keeps
		// byte-parity for any page that opens on a blank line or indentation.
		res.Files[out] = phpLtrim(r.sb.String())
		res.Deps[out] = deps.list()
		res.Diagnostics = append(res.Diagnostics, r.diags...)
	}
	// Applied once, over every diagnostic source (data, parse, render): see
	// dedupeDiagnostics' doc comment in diagnostics.go for why this needs to be
	// global rather than per-source.
	res.Diagnostics = dedupeDiagnostics(res.Diagnostics)
	return res
}

// variantBindings is the page scope for one variant: the shared page bindings
// (site, collections, content, Options.Data) with the variant's own data laid
// over them, its keys winning on collision exactly as Options.Data does. The
// shared map is never mutated — every variant starts from the same base — and
// a variant without data reuses it outright.
func variantBindings(base map[string]expr.Value, data map[string]any) map[string]expr.Value {
	if len(data) == 0 {
		return base
	}
	bindings := make(map[string]expr.Value, len(base)+len(data))
	for k, v := range base {
		bindings[k] = v
	}
	for k, v := range data {
		bindings[k] = toValue(v)
	}
	return bindings
}

// renderer walks an entry's nodes, accumulating output and diagnostics. The
// `eat` boolean threaded through the walk carries the "swallow one leading
// newline" state produced by the previous construct.
type renderer struct {
	sb    strings.Builder
	diags []Diagnostic
	scope expr.Scope
	// loopDepth counts the enclosing @foreach loops so a @break/@continue with
	// no active loop can be diagnosed rather than mishandled.
	loopDepth int
	// signal carries a pending @break/@continue up to the innermost loop.
	signal loopSignal
	// ctx holds the render state shared across the whole Compile — the vfs,
	// the per-output deps set, and the parse cache — so component renders reach
	// the filesystem and record their files. Nil is fine for renderers that
	// never resolve a component (some unit tests construct one directly).
	ctx *renderCtx
	// depth is this renderer's component-nesting depth (a page is 0, a component
	// it renders is 1, …), tracked per render-stack to enforce the recursion cap.
	depth int
	// legalProps is the single @props node that is a valid declaration for the
	// file this renderer is rendering (nil for pages and for component files with
	// no leading @props). Any other Props node renders as a props error.
	legalProps *ast.Props
}

// renderNodes renders a body left to right, threading the newline-eat state, and
// returns the state left after the last node. A raised loop signal (from a
// @break/@continue) stops the walk immediately so it can bubble up to the
// innermost @foreach, which consumes it. An exhausted loop budget also stops
// the walk — unconditionally, at every nesting level — so an aborted render
// unwinds without rendering anything further (see consumeLoopIteration).
func (r *renderer) renderNodes(nodes []ast.Node, eat bool) bool {
	for _, n := range nodes {
		eat = r.renderNode(n, eat)
		if r.signal != sigNone || r.overBudget() {
			return eat
		}
	}
	return eat
}

// renderNode renders one node and returns whether the NEXT literal text should
// have one leading newline stripped.
func (r *renderer) renderNode(n ast.Node, eat bool) bool {
	switch node := n.(type) {
	case *ast.Text:
		text := node.Bytes
		if eat {
			text = stripLeadingNewline(text)
		}
		r.sb.WriteString(text)
		return false // literal text never eats the following newline

	case *ast.Echo:
		r.renderEcho(node)
		return echoEatsNewline

	case *ast.If:
		r.renderIf(node)
		return directiveEatsNewline

	case *ast.Foreach:
		r.renderForeach(node)
		return directiveEatsNewline

	case *ast.LoopJump:
		return r.renderLoopJump(node)

	case *ast.Component:
		r.renderComponent(node)
		return directiveEatsNewline // a component tag eats one following newline

	case *ast.Props:
		r.renderProps(node)
		return directiveEatsNewline // the @props line eats one following newline

	case *ast.ClassDir:
		r.renderCssDir(node.Raw, node.P, false)
		return directiveEatsNewline

	case *ast.StyleDir:
		r.renderCssDir(node.Raw, node.P, true)
		return directiveEatsNewline

	case *ast.Vite:
		r.renderVite(node)
		// @vite compiles to the same `<?php echo ...; ?>` close-tag shape as any
		// other directive (CompilesHelpers::compileVite), so it eats one following
		// newline unconditionally -- settled empirically against the oracle
		// (task-10-report.md), even when nothing renders (no resolved CSS entries).
		return directiveEatsNewline

	case *ast.Unknown:
		// Any directive outside the closed subset this engine structurally
		// understands (@auth, @php, @include, @extends, @section, @csrf, …).
		// The parser never pairs it with a body (see ast.Unknown's doc comment),
		// so its byte-faithful source is written LITERALLY and the surrounding
		// text — including whatever a real @auth/@endauth pair would have gated —
		// flows through untouched. This matches real Blade for UNREGISTERED
		// directives (the section library's inline `@media`/`@keyframes`/
		// `@container` and Designer imports' boundary `@word`s survive
		// byte-for-byte). Registered-but-app-tier directives (@auth, @php, …)
		// render literally here but would COMPILE in real Blade — so any page
		// that carries an app-tier diagnostic stays outside the byte-parity
		// promise (see the package doc); the diagnostic is what tells the app
		// tier to take over.
		r.sb.WriteString(node.Src)
		r.diags = append(r.diags, unknownDiagnostic(node))
		// Unlike every other directive this does NOT eat the following newline,
		// and it ends any eating the construct before it began. A directive eats
		// a newline because it compiles to a `<?php … ?>` close tag, and PHP
		// swallows a newline only when it comes straight after one. An
		// unregistered directive compiles to nothing: it is bytes on the page
		// like any other text, so the newline after it stays, and a close tag
		// before it has these bytes after it and not a newline. Settled against
		// the oracle: fixtures/unknown-directive-newline.
		return false

	default:
		// Defensive: every ast.Node variant is matched above. Do nothing and
		// stay transparent to eat so an unrecognized future node type cannot
		// panic or perturb surrounding whitespace.
		return eat
	}
}

// renderEcho evaluates and writes a {{ }} / {!! !!} echo. Escaped echoes run the
// value through Escape; both classes diagnose (with empty output) on error.
func (r *renderer) renderEcho(node *ast.Echo) {
	val, err := expr.Eval(node.Expr, r.scope)
	if err != nil {
		r.diagExprError(err, node.P)
		return
	}
	// A slot value is pre-rendered raw HTML (real Blade's HtmlString): both
	// {{ }} and {!! !!} emit it verbatim, never escaped.
	if rh, ok := val.(rawHTML); ok {
		r.sb.WriteString(string(rh))
		return
	}
	// The $attributes bag is Htmlable: e() returns toHtml() untouched, so both
	// echo forms emit the imploded ` key="value"` string raw (bound values were
	// escaped when the bag was built). A non-scalar attribute value is PHP's
	// array-to-string fatal → an expression error here.
	if bagv, ok := val.(*expr.AttrBag); ok {
		s, herr := bagv.HTMLString()
		if herr != nil {
			r.diag(node.P, KindError, "expression", herr.Error())
			return
		}
		r.sb.WriteString(s)
		return
	}
	s, serr := Stringify(val)
	if serr != nil {
		// Non-scalar value (array/map): PHP would not echo it as text.
		r.diag(node.P, KindError, "expression", serr.Error())
		return
	}
	if !node.Raw {
		s = Escape(s)
	}
	r.sb.WriteString(s)
}

// renderCssDir renders @class / @style: the argument expression (normally an
// array literal) evaluates in the current scope and compiles through
// Arr::toCssClasses / Arr::toCssStyles into `class="…"` / `style="…"` —
// emitted verbatim, no HTML escaping, exactly like CompilesConditionals'
// compileClass/compileStyle. A bare @class (no argument) is PHP's null →
// Arr::wrap → the empty list. An argument that fails to evaluate (or holds a
// value PHP could not cast while imploding) is diagnosed and renders nothing.
func (r *renderer) renderCssDir(raw string, p scanner.Pos, style bool) {
	var v expr.Value
	if strings.TrimSpace(raw) != "" {
		var err error
		v, err = expr.Eval(raw, r.scope)
		if err != nil {
			r.diagExprError(err, p)
			return
		}
	}
	var s string
	var err error
	if style {
		s, err = expr.ToCssStyles(v)
	} else {
		s, err = expr.ToCssClasses(v)
	}
	if err != nil {
		r.diag(p, KindError, "expression", err.Error())
		return
	}
	if style {
		r.sb.WriteString(`style="` + s + `"`)
	} else {
		r.sb.WriteString(`class="` + s + `"`)
	}
}

// renderIf evaluates branches in order, rendering the first truthy one (else the
// @else body). A branch whose condition fails to evaluate is diagnosed and
// treated as false. Each rendered body starts with eat=true because the @if /
// @elseif / @else directive that introduces it swallows the newline before it.
func (r *renderer) renderIf(node *ast.If) {
	for _, br := range node.Branches {
		cond, err := expr.Eval(br.Cond, r.scope)
		if err != nil {
			r.diagExprError(err, br.P)
			continue // condition error → branch is false
		}
		if expr.Truthy(cond) {
			r.renderNodes(br.Body, directiveEatsNewline)
			return
		}
	}
	r.renderNodes(node.Else, directiveEatsNewline)
}

// diagExprError maps an expr.Eval error to a diagnostic per the render contract:
// an undefined variable is an error/undefined-variable; a valid-but-unsupported
// PHP expression (SyntaxError with AppTier) is app-tier/expression; anything else
// (garbage syntax, ordered-array comparison, non-numeric unary) is
// error/expression.
//
// History: until the attribute bag was modeled (2026-08-13), an undefined
// `$attributes` was reclassified app-tier/$attributes, because the engine
// never bound the bag at all. Now buildComponentScope binds $attributes in
// every component scope, so the only place it can be undefined is a PAGE —
// where real Blade raises the same undefined-variable error this reports.
// The carve-out is gone; error/undefined-variable is the parity answer.
func (r *renderer) diagExprError(err error, p scanner.Pos) {
	// In a Studio-syntax file the variable was written without its "$" (title,
	// post.title), so the message names it the same way.
	if errors.Is(err, expr.ErrUndefinedVar) {
		msg := err.Error()
		var uv *expr.UndefinedVarError
		if isHTMLFile(p.File) && errors.As(err, &uv) {
			msg = "undefined variable " + uv.Name
		}
		r.diag(p, KindError, "undefined-variable", msg)
		return
	}
	// A missing object member mirrors real Blade's Undefined-property abort.
	if errors.Is(err, expr.ErrUndefinedProp) {
		msg := err.Error()
		var up *expr.UndefinedPropError
		if isHTMLFile(p.File) && errors.As(err, &up) {
			msg = "undefined property " + up.Name
		}
		r.diag(p, KindError, "undefined-property", msg)
		return
	}
	// `[]` on an object mirrors PHP's "Cannot use object of type stdClass as
	// array" fatal.
	if errors.Is(err, expr.ErrObjectAsArray) {
		r.diag(p, KindError, "object-as-array", err.Error())
		return
	}
	var se *expr.SyntaxError
	if errors.As(err, &se) && se.AppTier {
		// Valid PHP the evaluator does not model. In Blade that is the app
		// tier's to run; in an .html file nothing will ever run it (see diag),
		// so the message says what a template CAN do instead.
		r.diag(p, KindAppTier, "expression", say(p, err.Error(), htmlExprMessage(err.Error())))
		return
	}
	r.diag(p, KindError, "expression", err.Error())
}

func (r *renderer) diag(p scanner.Pos, kind DiagKind, construct, msg string) {
	// "App tier" means real Laravel can take over what this engine cannot
	// finish. Real Laravel cannot read an .html view, so an .html file has no
	// tier to fall to: what would be app-tier in Blade is an error here.
	if kind == KindAppTier && isHTMLFile(p.File) {
		kind = KindError
	}
	r.diags = append(r.diags, Diagnostic{
		File:      p.File,
		Line:      p.Line,
		Col:       p.Col,
		Kind:      kind,
		Construct: construct,
		Message:   msg,
	})
}

// stripLeadingNewline removes one leading line terminator (\r\n, \n, or \r), the
// same set PHP's close-tag rule consumes after `?>`.
func stripLeadingNewline(s string) string {
	switch {
	case len(s) >= 2 && s[0] == '\r' && s[1] == '\n':
		return s[2:]
	case len(s) >= 1 && (s[0] == '\n' || s[0] == '\r'):
		return s[1:]
	}
	return s
}

// escapesRoot reports whether a forward-slash path, once cleaned, points
// outside the site root: an absolute path, or one that walks above the root
// with a leading "..". Entry paths and derived output names are both checked
// through this so neither a read nor a write can leave the site tree.
func escapesRoot(p string) bool {
	clean := path.Clean(p)
	return path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../")
}

// outputName maps an entry path to its output file name. Pages lose the
// resources/views/pages/ prefix; any other view keeps its path relative to
// resources/views/. A Blade page's .blade.php becomes .html; a Studio-syntax
// page already ends in .html and keeps it, so pages/about.blade.php and
// pages/about.html both render to about.html.
func outputName(entry string) string {
	const pages = "resources/views/pages/"
	const views = "resources/views/"
	name := entry
	switch {
	case strings.HasPrefix(name, pages):
		name = name[len(pages):]
	case strings.HasPrefix(name, views):
		name = name[len(views):]
	}
	if strings.HasSuffix(name, ".blade.php") {
		name = strings.TrimSuffix(name, ".blade.php")
	} else {
		name = strings.TrimSuffix(name, ".html")
	}
	return name + ".html"
}

// toValue converts a decoded JSON value into the expr.Value closed type set.
// JSON numbers arrive as float64 and stay float64 — Stringify's shortest
// round-trip prints an integral float without a fractional part, matching PHP's
// echo of a JSON-decoded integer.
//
// A JSON OBJECT (map[string]any) becomes an *expr.Object with stdClass
// semantics, so protocol/Options `data` objects behave like $site does. Their
// keys are emitted in SORTED order: this map[string]any has already been decoded
// by encoding/json, which discards JSON key order, so sorted is the only
// deterministic choice available here (file-sourced objects go through
// expr.DecodeJSON and keep true insertion order). The oracle decodes `--data`
// with assoc=true (PHP arrays), so data-object stdClass-vs-array edges are a
// documented, non-oracle-validated divergence — see protocol's package doc.
func toValue(v any) expr.Value {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		return x
	case string:
		return x
	case float64:
		return x
	case int:
		return int64(x)
	case int64:
		return x
	case []any:
		out := make([]expr.Value, len(x))
		for i, e := range x {
			out[i] = toValue(e)
		}
		return out
	case map[string]any:
		obj := expr.NewObject()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			obj.Set(k, toValue(x[k]))
		}
		return obj
	default:
		return nil
	}
}

// dataScope is a flat top-level variable scope over converted data.
type dataScope map[string]expr.Value

func (s dataScope) Lookup(name string) (expr.Value, bool) {
	v, ok := s[name]
	return v, ok
}
