// Package vfs abstracts site-source file access: disk for the CLI and
// oracle, in-memory for the JSON protocol. Paths are forward-slash,
// relative to the site root.
package vfs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FS interface {
	Read(path string) ([]byte, bool)
	List(prefix string) []string // sorted paths under prefix
}

type Mem map[string]string

func (m Mem) Read(path string) ([]byte, bool) {
	s, ok := m[path]
	return []byte(s), ok
}

func (m Mem) List(prefix string) []string {
	var out []string
	for p := range m {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

type Dir string

func (d Dir) Read(path string) ([]byte, bool) {
	full, ok := d.resolve(path)
	if !ok {
		return nil, false // path escapes the root — refuse the read
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, false
	}
	return b, true
}

func (d Dir) List(prefix string) []string {
	root, ok := d.abs()
	if !ok {
		return nil
	}
	base, ok := d.resolve(prefix)
	if !ok {
		return nil // prefix escapes the root — list nothing
	}
	var out []string
	filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

// abs returns the site root as a cleaned absolute path.
func (d Dir) abs() (string, bool) {
	root, err := filepath.Abs(string(d))
	if err != nil {
		return "", false
	}
	return root, true
}

// resolve joins a forward-slash path onto the root and confirms the result
// stays within the root after cleaning to absolute form. A path that escapes —
// via "..", or because it is itself absolute — is refused (ok=false), so a
// crafted entry, an @vite('../secret'), or a component reference cannot reach a
// file outside the site tree. Symlinks are not followed (filepath.Abs only
// cleans); the guard is a lexical containment check, matched on both sides.
func (d Dir) resolve(p string) (string, bool) {
	root, ok := d.abs()
	if !ok {
		return "", false
	}
	full, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(p)))
	if err != nil {
		return "", false
	}
	if full == root {
		return full, true // the root itself (e.g. an empty List prefix)
	}
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}
