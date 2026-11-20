// Package protocol implements DevDojo Framework's embedding contract: one JSON
// document on stdin describing a site (its source files, optional extra
// data, optional entry list), and one JSON document on stdout carrying the
// rendered output, the dependency graph, and diagnostics. It is the seam
// hosts (the DevDojo control plane, the `devdojo compile` CLI, or a
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
// Diagnostic kinds: a diagnostic's `kind` is passed through verbatim — the
// protocol does not validate it against an enum, so the set is additive and a
// host iterates whatever kinds arrive. This compile path emits only `error` and
// `app-tier`; the `warning` kind (undeclared component attributes) is produced
// solely by `devdojo lint` (render.Analyze), never here. That separation is
// deliberate: hosts that treat ANY diagnostic in this stream as a hard failure
// (the devdojo Sites template-sync gate) stay correct because a warning can
// never reach it — see render.DiagKind.
//
// options.url is accepted on Input but UNUSED in v1: relative URLs are the
// only rendering mode the engine supports, so the field round-trips without
// affecting output. It exists so a future canonical-URL rendering mode (or a
// host that already sends it, mirroring sitebuilder) has somewhere to land
// without an Input shape change.
//
// options.max_loop_iterations tunes the per-entry render budget: the total
// @foreach iterations one entry may run (across nesting, components, and
// slots) before its render aborts with a single error/render-budget
// diagnostic. Omitted or zero applies render.DefaultMaxLoopIterations — a
// bound no legitimate site approaches; hosts running untrusted or
// machine-generated templates can lower it.
package protocol

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/thedevdojo/framework/blade"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/vfs"
)

// pagesPrefix is where entry discovery looks when Input.Entries is omitted,
// and what `devdojo lint <dir>` compiles.
const pagesPrefix = "resources/views/pages/"

// Input is the stdin document: the site's source files, optional data to
// merge over the site's own data sources, an optional explicit entry list,
// and forward-compatible render options.
type Input struct {
	Files   map[string]string `json:"files"`
	Data    map[string]any    `json:"data,omitempty"`
	Entries []string          `json:"entries,omitempty"` // empty (and no variants) → every page under resources/views/pages/ (**.blade.php and **.html), sorted
	// Variants renders one entry many times — a Folio-style dynamic page, one
	// document per collection entry — in ONE request: each variant's data is
	// merged over `data` for that render alone, and its output lands at
	// `output` (the entry's usual output name when omitted). Entries and
	// variants may be combined; every entry renders first.
	Variants []Variant `json:"variants,omitempty"`
	Options  struct {
		URL string `json:"url,omitempty"` // accepted but UNUSED in v1; reserved for canonical-URL rendering — see package doc
		// MaxLoopIterations caps the total @foreach iterations one entry's
		// render may run before it aborts with an error/render-budget
		// diagnostic. Zero (or omitted) means render.DefaultMaxLoopIterations;
		// there is no unlimited setting — see render.Options.
		MaxLoopIterations int `json:"max_loop_iterations,omitempty"`
	} `json:"options"`
}

// Output is the stdout document. All four keys are always present: Files and
// Deps marshal as `{}` (never `null`) when empty, and Diagnostics marshals
// as `[]` — a host iterates these without a nil-check.
// Variant is one render of `entry` with `data` bound over the request's data,
// written to `output`. See Input.Variants.
type Variant struct {
	Entry  string         `json:"entry"`
	Data   map[string]any `json:"data,omitempty"`
	Output string         `json:"output,omitempty"`
}

type Output struct {
	Engine      string              `json:"engine"`
	Files       map[string]string   `json:"files"`
	Deps        map[string][]string `json:"deps"`
	Diagnostics []render.Diagnostic `json:"diagnostics"`
}

// pageExts are the extensions a page file may have, one per syntax: Blade
// (.blade.php) and Studio (.html). The order is the precedence when a route has
// both — the Blade page serves it — and matches the order the renderer names
// the two files in its page-collision diagnostic.
var pageExts = []string{".blade.php", ".html"}

// isPage reports whether p has a page extension.
func isPage(p string) bool {
	for _, ext := range pageExts {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// DiscoverPages returns every page under resources/views/pages/ in fs — each
// **.blade.php (Blade syntax) and **.html (Studio syntax) path — sorted. This is
// the default entry set when a protocol Input omits entries, and it is what
// `devdojo lint <dir>` compiles — both callers share it so "what counts as
// a page" has one definition.
func DiscoverPages(fs vfs.FS) []string {
	var out []string
	for _, p := range fs.List(pagesPrefix) {
		if isPage(p) {
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
	if len(entries) == 0 && len(input.Variants) == 0 {
		entries = DiscoverPages(fs)
	}
	variants := make([]render.Variant, 0, len(entries)+len(input.Variants))
	for _, entry := range entries {
		variants = append(variants, render.Variant{Entry: entry})
	}
	for _, v := range input.Variants {
		variants = append(variants, render.Variant{Entry: v.Entry, Data: v.Data, Output: v.Output})
	}

	result := render.CompileVariants(fs, variants, render.Options{
		Data:              input.Data,
		MaxLoopIterations: input.Options.MaxLoopIterations,
	})
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
