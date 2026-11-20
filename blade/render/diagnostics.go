package render

import (
	"github.com/thedevdojo/framework/blade/ast"
	"github.com/thedevdojo/framework/blade/parser"
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
		Message:   "directive @" + node.Name + " is outside the DevDojo Framework subset; requires the PHP app tier",
	}
}

// foldParseDiags maps parser.Diag values (recoverable parse errors from a page
// or component file) into the render Diagnostic contract: Kind "error",
// File/Line/Col/Message carried over verbatim, Construct carrying the specific
// construct the parser knows ("@if", "@foreach", "<x-hero>", "<x-slot:head>",
// …) with "parse" as the fallback for a diag with no construct. Returns nil
// for an empty/nil input so callers can append the result unconditionally.
func foldParseDiags(diags []parser.Diag) []Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]Diagnostic, len(diags))
	for i, d := range diags {
		construct := d.Construct
		if construct == "" {
			construct = "parse"
		}
		out[i] = Diagnostic{
			File:      d.File,
			Line:      d.Line,
			Col:       d.Col,
			Kind:      KindError,
			Construct: construct,
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

// entryInvalidDiagnostic reports an entries[] path that escapes the site root —
// its cleaned form is absolute or walks above the root with "..", or the output
// name it would derive does. Such an entry is refused before any read or write
// so a crafted path cannot read a file outside the tree or write output above
// the destination directory. Like entry-missing, the File IS the problem, so
// Line/Col are 1/1 by convention and no output file is produced.
func entryInvalidDiagnostic(entry string) Diagnostic {
	return Diagnostic{
		File:      entry,
		Line:      1,
		Col:       1,
		Kind:      KindError,
		Construct: "entry-invalid",
		Message:   "entry path escapes the site root: " + entry,
	}
}

// pageCollisionDiagnostic reports two different pages that render to the same
// output file — pages/index.blade.php beside pages/index.html. The later one
// still renders (a later output replaces an earlier one, as it always has), but
// one of the two files is dead weight the author should know about. It points
// at the second page; Line/Col are 1/1 because the problem is the file itself.
func pageCollisionDiagnostic(out, first, second string) Diagnostic {
	return Diagnostic{
		File:      second,
		Line:      1,
		Col:       1,
		Kind:      KindError,
		Construct: "page-collision",
		Message:   "two pages render to " + out + ": " + first + " and " + second + " — keep one",
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
