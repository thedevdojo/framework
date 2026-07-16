// Package protocol implements Pocketknife's embedding contract: one JSON
// document on stdin describing a site (its source files, optional extra
// data, optional entry list), and one JSON document on stdout carrying the
// rendered output, the dependency graph, and diagnostics. It is the seam
// hosts (the DevDojo control plane, the `pocketknife compile` CLI, or a
// future language binding) use to drive the Go engine without linking it
// directly — see the design spec's §6, which pins this shape deliberately
// close to sitebuilder's own JSON protocol so host wrappers transfer with
// minimal changes.
//
// Data values are arbitrary JSON. Scalars, arrays, and objects all decode
// through render.Options.Data into the expr package's Value type, and a JSON
// object becomes an *expr.Object (stdClass semantics) — so
// `"data": {"nav": {"label": "Home"}}` supports `{{ $nav->label }}` property
// access on the Go side exactly as it would for $site. One caveat: this map is
// decoded by encoding/json before it reaches the engine, which discards JSON
// key order, so a data object iterates its properties in SORTED order (a
// file-sourced object — $site, a collection — keeps document order via
// expr.DecodeJSON). The PHP oracle side decodes its own `--data` flag with
// assoc=true, turning objects into PHP arrays rather than stdClass; so the
// stdClass-vs-array edges (empty-object truthiness, `[]`-access) on DATA objects
// are a documented divergence not validated against the oracle — the object
// value model exists to match the SiteData bindings, which the oracle DOES model
// as stdClass.
//
// options.url is accepted on Input but UNUSED in v1: relative URLs are the
// only rendering mode the engine supports, so the field round-trips without
// affecting output. It exists so a future canonical-URL rendering mode (or a
// host that already sends it, mirroring sitebuilder) has somewhere to land
// without an Input shape change.
package protocol

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/thedevdojo/pocketknife/blade"
	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/vfs"
)

// pagesPrefix is where entry discovery looks when Input.Entries is omitted,
// and what `pocketknife lint <dir>` compiles.
const pagesPrefix = "resources/views/pages/"

// Input is the stdin document: the site's source files, optional data to
// merge over the site's own data sources, an optional explicit entry list,
// and forward-compatible render options.
type Input struct {
	Files   map[string]string `json:"files"`
	Data    map[string]any    `json:"data,omitempty"`
	Entries []string          `json:"entries,omitempty"` // empty → every resources/views/pages/**.blade.html, sorted
	Options struct {
		URL string `json:"url,omitempty"` // accepted, unused in v1 — see package doc
	} `json:"options"`
}

// Output is the stdout document. All four keys are always present: Files and
// Deps marshal as `{}` (never `null`) when empty, and Diagnostics marshals
// as `[]` — a host iterates these without a nil-check.
type Output struct {
	Engine      string              `json:"engine"`
	Files       map[string]string   `json:"files"`
	Deps        map[string][]string `json:"deps"`
	Diagnostics []render.Diagnostic `json:"diagnostics"`
}

// DiscoverPages returns every resources/views/pages/**.blade.html path in fs,
// sorted. This is the default entry set when a protocol Input omits entries,
// and it is what `pocketknife lint <dir>` compiles — both callers share it so
// "what counts as a page" has one definition.
func DiscoverPages(fs vfs.FS) []string {
	var out []string
	for _, p := range fs.List(pagesPrefix) {
		if strings.HasSuffix(p, ".blade.html") {
			out = append(out, p)
		}
	}
	return out
}

// Run decodes exactly one JSON document from in (via a single json.Decoder
// call — trailing bytes after the document, including trailing whitespace,
// are never inspected), compiles it through the render engine, and encodes
// the Output to out with a trailing newline.
//
// Malformed input JSON does NOT surface as a Go error: a host driving this
// protocol must always get back a parseable Output document, so a decode
// failure instead becomes a single Diagnostic{Kind: "error", Construct:
// "protocol"} inside an otherwise-empty Output. Run only returns a non-nil
// error when writing the output itself fails (an I/O failure) — never for a
// problem with the input's content.
func Run(in io.Reader, out io.Writer) error {
	var input Input
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return encode(out, protocolErrorOutput(err))
	}

	fs := vfs.Mem(input.Files)
	if fs == nil {
		fs = vfs.Mem{}
	}
	entries := input.Entries
	if len(entries) == 0 {
		entries = DiscoverPages(fs)
	}

	result := render.Compile(fs, entries, render.Options{Data: input.Data})
	return encode(out, Output{
		Engine:      blade.Version,
		Files:       nonNilStrings(result.Files),
		Deps:        nonNilDeps(result.Deps),
		Diagnostics: nonNilDiags(result.Diagnostics),
	})
}

// protocolErrorOutput builds the single-diagnostic Output emitted for input
// that fails to decode as JSON at all. File/Line/Col are the zero value: the
// problem is the envelope itself, not a position within any source file.
func protocolErrorOutput(err error) Output {
	return Output{
		Engine: blade.Version,
		Files:  map[string]string{},
		Deps:   map[string][]string{},
		Diagnostics: []render.Diagnostic{
			{
				Kind:      render.KindError,
				Construct: "protocol",
				Message:   "malformed input JSON: " + err.Error(),
			},
		},
	}
}

// encode writes v as one JSON document followed by a trailing newline.
// HTML escaping is disabled: rendered output routinely contains `<`, `>`,
// and `&`, and a host piping this through `jq` or diffing it against a
// golden file wants those bytes literal, not escaped to <-style
// sequences.
func encode(out io.Writer, v Output) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// nonNilStrings/nonNilDeps/nonNilDiags guarantee Output's map/slice fields
// marshal as `{}`/`{}`/`[]` rather than `null`. render.Compile already
// initializes Files and Deps, but Diagnostics is nil whenever a Compile
// finds nothing to report (dedupeDiagnostics returns its nil input
// unchanged below two elements) — guard all three defensively so the
// contract holds even if that changes upstream.
func nonNilStrings(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilDeps(m map[string][]string) map[string][]string {
	if m == nil {
		return map[string][]string{}
	}
	return m
}

func nonNilDiags(d []render.Diagnostic) []render.Diagnostic {
	if d == nil {
		return []render.Diagnostic{}
	}
	return d
}
