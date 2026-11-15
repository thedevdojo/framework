package convert

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/vfs"
)

// TestCorpus runs the converter over real sites. Point POCKETKNIFE_CORPUS at a
// folder of them — each site either the folder itself or its files/ subfolder —
// and every one is converted to the Studio syntax, back to Blade, and round
// again, in memory; nothing on disk is touched.
//
// Each conversion is refused unless the site renders byte for byte as before,
// so "it converted" already means "it renders the same". On top of that the
// test checks the trip settles: the second time round must reproduce the
// first exactly, in both syntaxes. (The first trip may tidy a Blade file —
// 'items' becomes 'items' => null, @vite('a') becomes @vite(['a']) — so the
// original is compared for information only.) A site that is refused is
// logged with the reason. Skipped when the variable is not set.
//
//	POCKETKNIFE_CORPUS=~/devdojo/devdojo/resources/sites-templates go test ./blade/convert -run TestCorpus -v
func TestCorpus(t *testing.T) {
	root := os.Getenv("POCKETKNIFE_CORPUS")
	if root == "" {
		t.Skip("POCKETKNIFE_CORPUS is not set")
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	sites, converted, untouched, files := 0, 0, 0, 0
	reasons := map[string]int{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		if _, err := os.Stat(filepath.Join(dir, "files", "resources", "views")); err == nil {
			dir = filepath.Join(dir, "files")
		} else if _, err := os.Stat(filepath.Join(dir, "resources", "views")); err != nil {
			continue
		}
		sites++
		b0 := loadSite(t, dir)

		// step converts a site and reports whether it went through. A refusal
		// on the way out (Blade a site cannot leave) is information; one on
		// any later step is a converter bug.
		step := func(site vfs.Mem, to Syntax, label string, fatal bool) (vfs.Mem, bool) {
			res := Site(site, to, render.Options{})
			for _, p := range res.Problems {
				reasons[p.Message]++
				if fatal {
					t.Errorf("%s %s: %s:%d:%d %s", d.Name(), label, p.File, p.Line, p.Col, p.Message)
				} else {
					t.Logf("%s %s: %s:%d:%d %s", d.Name(), label, p.File, p.Line, p.Col, p.Message)
				}
			}
			if len(res.Problems) > 0 {
				return nil, false
			}
			if label == "→ html" {
				files += len(res.Files)
			}
			return apply(site, res), true
		}
		h1, ok := step(b0, Studio, "→ html", false)
		if !ok {
			continue
		}
		b1, ok := step(h1, Blade, "→ html → blade", true)
		if !ok {
			continue
		}
		h2, ok := step(b1, Studio, "→ html → blade → html", true)
		if !ok {
			continue
		}
		b2, ok := step(h2, Blade, "→ html → blade → html → blade", true)
		if !ok {
			continue
		}
		settled := sameSite(t, d.Name()+" (Studio, second trip)", h1, h2) && sameSite(t, d.Name()+" (Blade, second trip)", b1, b2)
		if settled {
			converted++
		}
		same := true
		for p, body := range b0 {
			if b1[p] != body {
				same = false
			}
		}
		if same {
			untouched++
		}
	}
	t.Logf("%d of %d sites convert to the Studio syntax and back, rendering the same (%d files)", converted, sites, files)
	t.Logf("%d of those come back to the very same Blade bytes on the first trip", untouched)
	var keys []string
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%4d × %s", reasons[k], k)
	}
}

// sameSite reports whether two sites hold the same files, failing the test at
// the first line that differs.
func sameSite(t *testing.T, label string, a, b vfs.Mem) bool {
	t.Helper()
	same := len(a) == len(b)
	for p, body := range a {
		if b[p] != body {
			same = false
			line, _ := firstDifference(body, b[p])
			t.Errorf("%s: %s changed (line %d)\n  was: %s\n  now: %s", label, p, line, lineOf(body, line), lineOf(b[p], line))
		}
	}
	return same
}

// loadSite reads a site folder into memory.
func loadSite(t *testing.T, dir string) vfs.Mem {
	t.Helper()
	site := vfs.Mem{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if name := info.Name(); name == "node_modules" || name == "vendor" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "resources/") && !strings.HasPrefix(rel, "content/") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		site[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return site
}

// lineOf returns the 1-based line n of s.
func lineOf(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}
