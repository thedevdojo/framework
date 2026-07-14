package render

import (
	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/parser"
)

// This file holds the diagnostic-shaping logic Task 11 adds on top of the
// render pipeline: classifying an ast.Unknown directive as app-tier, folding
// parser.Diag values (from page AND component-file parses) into the
// render.Diagnostic contract, flagging a missing entry, and deduping the
// final list. Everything here is pure data shaping — no rendering.

// unknownDiagnostic classifies an ast.Unknown node — any directive outside the
// closed Blade subset this engine structurally understands (@auth, @php,
// @include, @extends, @section, @csrf, …) — as app-tier: valid Blade the
// engine cannot execute, so it falls back to the real PHP app tier. Construct
// is "@"+Name (e.g. "@auth", "@endauth") so a page's diagnostics name exactly
// which directive needs the app tier.
func unknownDiagnostic(node *ast.Unknown) Diagnostic {
	return Diagnostic{
		File:      node.P.File,
		Line:      node.P.Line,
		Col:       node.P.Col,
		Kind:      KindAppTier,
		Construct: "@" + node.Name,
		Message:   "directive @" + node.Name + " is outside the Pocketknife subset; requires the PHP app tier",
	}
}

// foldParseDiags maps parser.Diag values (recoverable parse errors from a page
// or component file) into the render Diagnostic contract: Kind "error",
// Construct "parse", File/Line/Col/Message carried over verbatim. Returns nil
// for an empty/nil input so callers can append the result unconditionally.
func foldParseDiags(diags []parser.Diag) []Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]Diagnostic, len(diags))
	for i, d := range diags {
		out[i] = Diagnostic{
			File:      d.File,
			Line:      d.Line,
			Col:       d.Col,
			Kind:      KindError,
			Construct: "parse",
			Message:   d.Message,
		}
	}
	return out
}

// entryMissingDiagnostic reports an entries[] path that does not resolve in
// the filesystem: no output file is produced for it. A missing file has no
// position within it to point at, so Line/Col are 1/1 by convention — the
// diagnostic's File IS the problem.
func entryMissingDiagnostic(entry string) Diagnostic {
	return Diagnostic{
		File:      entry,
		Line:      1,
		Col:       1,
		Kind:      KindError,
		Construct: "entry-missing",
		Message:   "entry not found: " + entry,
	}
}

// diagKey identifies a diagnostic for dedupe purposes.
type diagKey struct {
	file, construct string
	kind            DiagKind
	line, col       int
}

// dedupeDiagnostics collapses diagnostics that share (File, Line, Col, Kind,
// Construct), keeping the first occurrence (and its Message) and otherwise
// preserving order. Applied once at Result assembly (Compile) so every
// diagnostic source — data loading, parsing, rendering — is covered without
// each source needing its own bookkeeping. Two situations produce true
// duplicates by this tuple: a component file re-rendered from multiple call
// sites (a stray directive at the same file:line:col is still one problem in
// the source, even though the render walk visits it once per call site), and
// a @foreach body whose every iteration re-evaluates the same construct at
// the same source position (one bad expression, not one diagnostic per
// element).
func dedupeDiagnostics(ds []Diagnostic) []Diagnostic {
	if len(ds) < 2 {
		return ds
	}
	seen := make(map[diagKey]struct{}, len(ds))
	out := make([]Diagnostic, 0, len(ds))
	for _, d := range ds {
		k := diagKey{file: d.File, construct: d.Construct, kind: d.Kind, line: d.Line, col: d.Col}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, d)
	}
	return out
}
