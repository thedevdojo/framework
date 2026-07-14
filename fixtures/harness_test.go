package fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/vfs"
)

type caseSpec struct {
	Entries     []string         `json:"entries"`
	Data        map[string]any   `json:"data"`
	Oracle      *bool            `json:"oracle"`
	Diagnostics []map[string]any `json:"diagnostics"`
}

func TestFixtures(t *testing.T) {
	dirs, _ := os.ReadDir(".")
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(name, "case.json"))
			if err != nil {
				t.Fatalf("case.json: %v", err)
			}
			var spec caseSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatalf("case.json: %v", err)
			}
			res := render.Compile(vfs.Dir(filepath.Join(name, "input")), spec.Entries, render.Options{Data: spec.Data})
			// Compare every expected/** file byte-for-byte.
			expRoot := filepath.Join(name, "expected")
			filepath.Walk(expRoot, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(expRoot, p)
				rel = filepath.ToSlash(rel)
				want, _ := os.ReadFile(p)
				got, ok := res.Files[rel]
				if !ok {
					t.Errorf("missing output %s (have %v)", rel, keys(res.Files))
					return nil
				}
				if got != string(want) {
					t.Errorf("%s mismatch:\n--- want ---\n%s\n--- got ---\n%s", rel, want, got)
				}
				return nil
			})
			// Expected diagnostics: match on kind+construct+file+line.
			for _, wd := range spec.Diagnostics {
				if !hasDiag(res.Diagnostics, wd) {
					t.Errorf("expected diagnostic %v not found in %v", wd, res.Diagnostics)
				}
			}
		})
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasDiag(ds []render.Diagnostic, want map[string]any) bool {
	for _, d := range ds {
		if string(d.Kind) == want["kind"] &&
			(want["construct"] == nil || d.Construct == want["construct"]) &&
			(want["file"] == nil || d.File == want["file"]) &&
			(want["line"] == nil || float64(d.Line) == want["line"]) {
			return true
		}
	}
	return false
}

var _ = strings.TrimSpace // keep imports honest until fleshed out
