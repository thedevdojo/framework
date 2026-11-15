// Package pocketknife is the root facade over the HTML compiler's engine — the
// single public Go entry point a host embeds. It re-exports the render
// pipeline's Compile call and its input/output types so callers depend on
// `github.com/thedevdojo/pocketknife` rather than reaching into the internal
// `blade/render` subpackage. This is the API spec §4 promises:
// `pocketknife.Compile(fs, entries, opts) → files, deps, diagnostics`.
//
// Everything here is a thin alias or forwarder — the behavior lives in
// blade/render (and the CLI in cmd/pocketknife, the JSON contract in protocol).
package pocketknife

import (
	"github.com/thedevdojo/pocketknife/blade"
	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/vfs"
)

// Version is the engine semver (blade.Version), surfaced so a host can pin or
// detect engine capability without importing the blade package.
const Version = blade.Version

// FS is the source-file resolver a Compile reads from. vfs.Dir backs the CLI
// and oracle (disk); vfs.Mem backs the JSON protocol (in-memory).
type FS = vfs.FS

// Options carries render inputs — chiefly the protocol's "data" map, merged
// over the site's own data sources ($site, collections, content).
type Options = render.Options

// Result is a Compile's output: rendered files by output name, the source
// files each output depended on (the render-on-save graph), and diagnostics.
type Result = render.Result

// Diagnostic is one problem found while rendering, positioned at its source
// file/line/col and classified by Kind.
type Diagnostic = render.Diagnostic

// DiagKind classifies a Diagnostic.
type DiagKind = render.DiagKind

// Diagnostic kinds, re-exported. KindWarning is produced only by Analyze
// (lint), never by Compile — see the protocol package doc and render.DiagKind.
const (
	KindError   = render.KindError
	KindAppTier = render.KindAppTier
	KindWarning = render.KindWarning
)

// Compile renders each entry through the engine and returns the collected
// files, deps, and diagnostics. Missing entries are skipped; Compile never
// panics on arbitrary input.
func Compile(fs FS, entries []string, opts Options) Result {
	return render.Compile(fs, entries, opts)
}

// Analyze is the lint-only analysis pass: it returns WARNING-kind diagnostics
// (undeclared component attributes) that Compile deliberately never emits, so
// the compile diagnostic stream stays free of warnings. `pocketknife lint`
// surfaces these; a host that wants the same hints calls Analyze directly.
func Analyze(fs FS, entries []string) []Diagnostic {
	return render.Analyze(fs, entries)
}
