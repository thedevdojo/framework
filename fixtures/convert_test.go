package fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/convert"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/vfs"
)

// unconvertible lists every fixture that `devdojo convert` must REFUSE,
// keyed "fixture → syntax", with the reason. Everything not listed here must
// convert — and a listed case that starts converting fails the test, so the
// list cannot quietly go stale.
var unconvertible = map[string]string{
	"app-tier-directives → studio":             "@auth, route() and an interpolated attribute all need real Laravel",
	"props-array-default-unsupported → studio": "a nested @props default needs real Laravel",
	"html-diagnostics → blade":                 "the page is full of deliberate mistakes (<If> with no condition, <Layout> with no src)",
	"html-foreach-limit → blade":               "limit with loop.count and loop.last: Blade's @break does not shorten the list",
}

// TestFixturesConvert runs every fixture through the converter. The fixture's
// site is converted to the other syntax and rendered: it must produce the
// fixture's expected/ tree, the bytes real Laravel produced for the original.
// Then it is converted back and rendered again, against the same bytes.
func TestFixturesConvert(t *testing.T) {
	dirs, _ := os.ReadDir(".")
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		raw, err := os.ReadFile(filepath.Join(name, "case.json"))
		if err != nil {
			continue
		}
		var spec caseSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			t.Fatalf("%s/case.json: %v", name, err)
		}
		files, _ := readTree(filepath.Join(name, "input"))
		want, _ := readTree(filepath.Join(name, "expected"))
		for _, to := range []convert.Syntax{convert.Studio, convert.Blade} {
			from := convert.Blade
			if to == convert.Blade {
				from = convert.Studio
			}
			if !hasViews(files, from) {
				continue
			}
			key := name + " → " + string(to)
			t.Run(key, func(t *testing.T) {
				opts := render.Options{Data: spec.Data}
				site := vfs.Mem{}
				for p, body := range files {
					site[p] = body
				}
				res := convert.Site(site, to, opts)
				if reason, listed := unconvertible[key]; listed {
					if len(res.Problems) == 0 {
						t.Errorf("listed as unconvertible (%s) but it converted; remove it from the list", reason)
					}
					return
				}
				if len(res.Problems) > 0 {
					t.Fatalf("did not convert:\n%s", problemLines(res.Problems))
				}
				converted, entries := carryOut(site, spec.Entries, res)
				checkRender(t, "converted", converted, entries, opts, want)

				back := convert.Site(converted, from, opts)
				if len(back.Problems) > 0 {
					t.Fatalf("did not convert back:\n%s", problemLines(back.Problems))
				}
				again, entries := carryOut(converted, entries, back)
				checkRender(t, "converted back", again, entries, opts, want)
			})
		}
	}
}

// hasViews reports whether a site has any view written in syntax.
func hasViews(files map[string]string, syntax convert.Syntax) bool {
	for p := range files {
		if strings.HasPrefix(p, "resources/views/") && strings.HasSuffix(p, syntax.Ext()) {
			return true
		}
	}
	return false
}

// carryOut applies a conversion to a site and renames the entries it moved.
func carryOut(site vfs.Mem, entries []string, res convert.Result) (vfs.Mem, []string) {
	out := vfs.Mem{}
	for p, body := range site {
		out[p] = body
	}
	moved := map[string]string{}
	for _, old := range res.Removed {
		delete(out, old)
	}
	for p, body := range res.Files {
		out[p] = body
	}
	for _, old := range res.Removed {
		for p := range res.Files {
			if stem(p) == stem(old) {
				moved[old] = p
			}
		}
	}
	renamed := make([]string, len(entries))
	for i, e := range entries {
		renamed[i] = e
		if to, ok := moved[e]; ok {
			renamed[i] = to
		}
	}
	return out, renamed
}

// stem is a view path without its syntax extension.
func stem(p string) string {
	return strings.TrimSuffix(strings.TrimSuffix(p, ".blade.php"), ".html")
}

func checkRender(t *testing.T, label string, site vfs.Mem, entries []string, opts render.Options, want map[string]string) {
	t.Helper()
	got := render.Compile(site, entries, opts)
	for rel, body := range want {
		if got.Files[rel] != body {
			t.Errorf("%s: %s renders differently:\n--- want ---\n%s\n--- got ---\n%s", label, rel, body, got.Files[rel])
		}
	}
}

func problemLines(problems []render.Diagnostic) string {
	lines := make([]string, len(problems))
	for i, p := range problems {
		lines[i] = "  " + p.File + ":" + itoa(p.Line) + ":" + itoa(p.Col) + " " + p.Message
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
