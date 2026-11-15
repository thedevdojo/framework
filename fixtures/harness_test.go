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
	// Deps, when declared, pins an output's dependency list EXACTLY (order
	// included). Deps record every source path the render used or LOOKED FOR
	// (a missing component's candidate path, a missing @vite asset), so a
	// render-on-save host re-renders once the user creates the missing file.
	// The oracle ignores this key: deps are a Go-engine contract, not a Blade
	// output.
	Deps map[string][]string `json:"deps"`
	// Twin names the Blade fixture this one is the Studio-syntax port of. The
	// PHP oracle cannot render an .html view, so an HTML fixture borrows its
	// proof: its expected/ tree must be byte-identical to its twin's, and the
	// twin's is what real Laravel Blade produced. The same page written in the
	// two syntaxes renders the same bytes, or this harness fails.
	Twin string `json:"twin"`
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
			checkHTMLCase(t, name, spec)
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
			// Expected diagnostics: the EXACT set, order-insensitive. Each
			// expected entry claims one actual diagnostic (matching on kind,
			// plus construct/file/line/col when declared); an actual
			// diagnostic no entry claims fails the fixture, so stray or
			// duplicate diagnostics cannot accumulate unseen.
			claimed := make([]bool, len(res.Diagnostics))
			for _, wd := range spec.Diagnostics {
				found := false
				for i, d := range res.Diagnostics {
					if !claimed[i] && diagMatches(d, wd) {
						claimed[i] = true
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected diagnostic %v not found in %v", wd, res.Diagnostics)
				}
			}
			for i, d := range res.Diagnostics {
				if !claimed[i] {
					t.Errorf("undeclared diagnostic %+v (declare it in case.json)", d)
				}
			}
			// Declared deps: exact list per output, order included.
			for out, want := range spec.Deps {
				got := res.Deps[out]
				if len(got) != len(want) {
					t.Errorf("deps[%s] = %v; want %v", out, got, want)
					continue
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("deps[%s] = %v; want %v", out, got, want)
						break
					}
				}
			}
		})
	}
}

// checkHTMLCase holds a Studio-syntax fixture to the two rules that keep it
// honest. A case with an .html entry must opt out of the PHP oracle (real
// Blade cannot read the Studio syntax, so the oracle would fail on it), and a
// case that names a twin must expect exactly what its twin expects.
func checkHTMLCase(t *testing.T, name string, spec caseSpec) {
	t.Helper()
	html := false
	for _, entry := range spec.Entries {
		if strings.HasSuffix(entry, ".html") {
			html = true
		}
	}
	if html && (spec.Oracle == nil || *spec.Oracle) {
		t.Errorf(`an .html entry needs "oracle": false in case.json — the PHP oracle only renders Blade`)
	}
	if spec.Twin == "" {
		return
	}
	if !html {
		t.Errorf(`"twin" is for Studio-syntax fixtures; this case has no .html entry`)
	}
	want, err := readTree(filepath.Join(spec.Twin, "expected"))
	if err != nil || len(want) == 0 {
		t.Fatalf("twin %q has no expected/ tree (%v)", spec.Twin, err)
	}
	got, _ := readTree(filepath.Join(name, "expected"))
	for rel, body := range want {
		mine, ok := got[rel]
		if !ok {
			t.Errorf("expected/%s is missing; twin %q has it", rel, spec.Twin)
			continue
		}
		if mine != body {
			t.Errorf("expected/%s differs from twin %q:\n--- twin ---\n%s\n--- this ---\n%s", rel, spec.Twin, body, mine)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Errorf("expected/%s is not in twin %q", rel, spec.Twin)
		}
	}
}

// readTree reads every file under root, keyed by its slash path below root.
func readTree(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	return out, err
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// diagMatches reports whether one actual diagnostic satisfies one expected
// entry: kind always compares; construct/file/line/col compare only when the
// entry declares them.
func diagMatches(d render.Diagnostic, want map[string]any) bool {
	return string(d.Kind) == want["kind"] &&
		(want["construct"] == nil || d.Construct == want["construct"]) &&
		(want["file"] == nil || d.File == want["file"]) &&
		(want["line"] == nil || float64(d.Line) == want["line"]) &&
		(want["col"] == nil || float64(d.Col) == want["col"])
}
