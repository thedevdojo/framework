// Package starter holds the site `devdojo new` writes: a copy of the starter
// template (github.com/devdojo-templates/starter) kept inside the binary.
//
// It is inside the binary, and not downloaded, for two reasons. `devdojo new`
// works with no network. And a version of the tool always writes a site that
// same version reads, because the tests compile this copy on every run.
//
// The copy is refreshed with `make starter STARTER=/path/to/starter`; nothing
// under site/ is edited here by hand.
package starter

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// `all:` so that a file whose name starts with a dot (.gitignore) comes too.
//
//go:embed all:site
var files embed.FS

// ErrNotEmpty is returned by Write for a folder that already has something in
// it. Nothing is written in that case.
var ErrNotEmpty = errors.New("the folder is not empty")

// FS returns the starter site, rooted at the site itself: src/pages/index.html,
// public/favicon.svg and so on.
func FS() fs.FS {
	sub, err := fs.Sub(files, "site")
	if err != nil {
		// fs.Sub only fails on an invalid name, and "site" is a constant.
		panic(err)
	}
	return sub
}

// Write writes the starter into dir and reports how many files it wrote. The
// folder is made when it does not exist. One that exists must be empty:
// someone's work is never written over, so anything in it is ErrNotEmpty.
func Write(dir string) (int, error) {
	info, err := os.Stat(dir)
	switch {
	case err == nil && !info.IsDir():
		return 0, fmt.Errorf("%s is a file, not a folder", dir)
	case err == nil:
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}
		if len(entries) > 0 {
			return 0, ErrNotEmpty
		}
	case !errors.Is(err, fs.ErrNotExist):
		return 0, err
	}

	count := 0
	err = fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(FS(), p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}
