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
	b, err := os.ReadFile(filepath.Join(string(d), filepath.FromSlash(path)))
	if err != nil {
		return nil, false
	}
	return b, true
}

func (d Dir) List(prefix string) []string {
	root := filepath.Join(string(d), filepath.FromSlash(prefix))
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(string(d), p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}
