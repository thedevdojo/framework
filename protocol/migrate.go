package protocol

import (
	"encoding/json"
	"io"

	"github.com/thedevdojo/framework/blade"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/migrate"
	"github.com/thedevdojo/framework/vfs"
)

// MigrateInput is the stdin document of `devdojo migrate` with no directory:
// the site's files, and the data its pages are normally rendered with (so the
// before-and-after check exercises the same branches).
//
// A host that keeps some files out of the request (pictures, say) still gets
// the right answer for the ones it sent, and moves the rest by the same rule:
// everything under the old source folder goes to the same place under src/,
// with pages and components losing views/.
type MigrateInput struct {
	Files   map[string]string `json:"files"`
	Data    map[string]any    `json:"data,omitempty"`
	Options struct {
		MaxLoopIterations int `json:"max_loop_iterations,omitempty"`
	} `json:"options"`
}

// MigrateOutput is the stdout document. Moves is every file that changes its
// path, sorted by where it was. Files holds new contents, by NEW path, for the
// files whose text changes: an .html view written before single braces, or a
// file whose text named an old path. A file can be in Files without moving;
// a moved file not listed there moves unchanged. When Diagnostics is not empty
// nothing is to be changed, and Moves and Files are empty. All four keys are
// always present.
type MigrateOutput struct {
	Engine      string              `json:"engine"`
	Moves       []migrate.Move      `json:"moves"`
	Files       map[string]string   `json:"files"`
	Diagnostics []render.Diagnostic `json:"diagnostics"`
}

// RunMigrate decodes one MigrateInput from in and writes one MigrateOutput to
// out. Like Run, a problem with the input is a diagnostic, never a Go error.
func RunMigrate(in io.Reader, out io.Writer) error {
	result := MigrateOutput{Engine: blade.Version, Moves: []migrate.Move{}, Files: map[string]string{}, Diagnostics: []render.Diagnostic{}}
	var input MigrateInput
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		result.Diagnostics = append(result.Diagnostics, render.Diagnostic{
			Kind: render.KindError, Construct: "protocol", Message: "malformed input JSON: " + err.Error(),
		})
	} else {
		fs := vfs.Mem(input.Files)
		if fs == nil {
			fs = vfs.Mem{}
		}
		res := migrate.Site(fs, render.Options{
			Data:              input.Data,
			MaxLoopIterations: input.Options.MaxLoopIterations,
		})
		if len(res.Moves) > 0 {
			result.Moves = res.Moves
		}
		if len(res.Files) > 0 {
			result.Files = res.Files
		}
		if len(res.Problems) > 0 {
			result.Diagnostics = res.Problems
		}
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(result)
}
