// Package render compiles the Blade-subset AST into rendered bytes. It is the
// point where Pocketknife's Go pipeline (scanner → parser → expr) meets real
// Blade's output contract: echoes are escaped with Laravel's e(), values are
// stringified the PHP-echo way, and the newline swallowed by a compiled `?>` is
// reproduced exactly — a rule settled empirically against the oracle, NOT from
// the brief's hypothesis (see .superpowers/sdd/task-6-report.md).
//
// Task 6 scope: Text, Echo (escaped + raw), and If are rendered; comments carry
// no node (the parser drops them); Foreach/LoopJump/Component/Props/Vite/Unknown
// are error-tolerant stubs that render nothing without a diagnostic — later
// tasks give them semantics. Compile never panics on arbitrary input.
package render

import (
	"errors"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/blade/parser"
	"github.com/thedevdojo/pocketknife/blade/scanner"
	"github.com/thedevdojo/pocketknife/vfs"
)

// DiagKind classifies a diagnostic. KindError marks something the engine cannot
// render (undefined variable, unrenderable expression); KindAppTier marks valid
// Blade that needs real PHP and so falls to the app tier.
type DiagKind string

const (
	KindError   DiagKind = "error"
	KindAppTier DiagKind = "app-tier"
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

// Options carries render inputs. Data is the protocol's "data" map (later merged
// over $site); its keys become top-level $variables.
type Options struct {
	Data map[string]any
}

// Result is the output of a Compile: rendered files by output name, the source
// files each output depended on, and any diagnostics gathered along the way.
type Result struct {
	Files       map[string]string   // output name → rendered bytes
	Deps        map[string][]string // output name → source files used
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

// Compile renders each entry through the scanner → parser → renderer pipeline
// and returns the collected files, deps, and diagnostics. Missing entries are
// skipped. Compile never panics.
func Compile(fs vfs.FS, entries []string, opts Options) Result {
	res := Result{
		Files: map[string]string{},
		Deps:  map[string][]string{},
	}
	scope := buildScope(opts.Data)
	for _, entry := range entries {
		src, ok := fs.Read(entry)
		if !ok {
			continue
		}
		out := outputName(entry)
		tokens := scanner.Scan(entry, src)
		nodes, _ := parser.Parse(entry, tokens) // parse diagnostics are surfaced in a later task
		r := &renderer{scope: scope}
		r.renderNodes(nodes, false)
		res.Files[out] = r.sb.String()
		res.Deps[out] = []string{entry}
		res.Diagnostics = append(res.Diagnostics, r.diags...)
	}
	return res
}

// renderer walks an entry's nodes, accumulating output and diagnostics. The
// `eat` boolean threaded through the walk carries the "swallow one leading
// newline" state produced by the previous construct.
type renderer struct {
	sb    strings.Builder
	diags []Diagnostic
	scope expr.Scope
}

// renderNodes renders a body left to right, threading the newline-eat state, and
// returns the state left after the last node.
func (r *renderer) renderNodes(nodes []ast.Node, eat bool) bool {
	for _, n := range nodes {
		eat = r.renderNode(n, eat)
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

	default:
		// Foreach, LoopJump, Component, Props, Vite, Unknown: stubs for now —
		// no output, no diagnostic. Transparent to the newline state so a stub
		// never changes surrounding whitespace before its real implementation.
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
func (r *renderer) diagExprError(err error, p scanner.Pos) {
	if errors.Is(err, expr.ErrUndefinedVar) {
		r.diag(p, KindError, "undefined-variable", err.Error())
		return
	}
	var se *expr.SyntaxError
	if errors.As(err, &se) && se.AppTier {
		r.diag(p, KindAppTier, "expression", err.Error())
		return
	}
	r.diag(p, KindError, "expression", err.Error())
}

func (r *renderer) diag(p scanner.Pos, kind DiagKind, construct, msg string) {
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

// outputName maps an entry path to its output file name. Pages lose the
// resources/views/pages/ prefix; any other view keeps its path relative to
// resources/views/. In both cases .blade.html becomes .html.
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
	name = strings.TrimSuffix(name, ".blade.html")
	return name + ".html"
}

// buildScope converts the protocol's decoded JSON data into an expr.Scope whose
// keys are top-level variables.
func buildScope(data map[string]any) expr.Scope {
	m := make(map[string]expr.Value, len(data))
	for k, v := range data {
		m[k] = toValue(v)
	}
	return dataScope(m)
}

// toValue converts a decoded JSON value into the expr.Value closed type set.
// JSON numbers arrive as float64 and stay float64 — Stringify's shortest
// round-trip prints an integral float without a fractional part, matching PHP's
// echo of a JSON-decoded integer.
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
		out := make(map[string]expr.Value, len(x))
		for k, e := range x {
			out[k] = toValue(e)
		}
		return out
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
