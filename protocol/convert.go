package protocol

import (
	"encoding/json"
	"io"

	"github.com/thedevdojo/pocketknife/blade"
	"github.com/thedevdojo/pocketknife/blade/convert"
	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/vfs"
)

// ConvertInput is the stdin document of `pocketknife convert`: a site's files
// and the syntax to convert its views to, "blade" or "html". Data and options
// mean what they mean to compile — they are what the site is rendered with,
// before and after, to prove the conversion changed nothing.
type ConvertInput struct {
	To      string            `json:"to"`
	Files   map[string]string `json:"files"`
	Data    map[string]any    `json:"data,omitempty"`
	Options struct {
		MaxLoopIterations int `json:"max_loop_iterations,omitempty"`
	} `json:"options"`
}

// ConvertOutput is the stdout document. Files are the views to write, by their
// new path; Removed are the views they replace. A site converts whole or not
// at all: when Diagnostics is not empty nothing was converted and both are
// empty. A site with no views in the other syntax is not an error — all three
// come back empty. Every key is always present, never null.
type ConvertOutput struct {
	Engine      string              `json:"engine"`
	Files       map[string]string   `json:"files"`
	Removed     []string            `json:"removed"`
	Diagnostics []render.Diagnostic `json:"diagnostics"`
}

// RunConvert is Run for conversion: one JSON document in, one out with a
// trailing newline. Like Run it only returns an error when the output cannot
// be written — a problem with the input comes back as a diagnostic.
func RunConvert(in io.Reader, out io.Writer) error {
	result := ConvertOutput{Engine: blade.Version, Files: map[string]string{}, Removed: []string{}, Diagnostics: []render.Diagnostic{}}
	var input ConvertInput
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		result.Diagnostics = append(result.Diagnostics, render.Diagnostic{
			Kind: render.KindError, Construct: "protocol", Message: "malformed input JSON: " + err.Error(),
		})
	} else {
		fs := vfs.Mem(input.Files)
		if fs == nil {
			fs = vfs.Mem{}
		}
		res := convert.Site(fs, convert.Syntax(input.To), render.Options{
			Data:              input.Data,
			MaxLoopIterations: input.Options.MaxLoopIterations,
		})
		if len(res.Files) > 0 {
			result.Files = res.Files
		}
		if len(res.Removed) > 0 {
			result.Removed = res.Removed
		}
		if len(res.Problems) > 0 {
			result.Diagnostics = res.Problems
		}
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(result)
}
