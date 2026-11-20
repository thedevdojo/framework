package protocol

import (
	"path"
	"strings"

	"github.com/thedevdojo/framework/vfs"
)

// ResolveURL maps a request URL path to the page entry that serves it, using
// the same convention as Folio and the classic engine (spec §2):
//
//	/            → resources/views/pages/index.blade.php
//	/about       → resources/views/pages/about.blade.php        (a flat file wins)
//	             → else resources/views/pages/about/index.blade.php
//	/blog        → resources/views/pages/blog/index.blade.php    (directory index)
//	(no match)   → resources/views/pages/404.blade.php           if present
//
// A page may equally be a Studio-syntax file: every row above also matches the
// same path ending in .html. The flat-wins rule spans both syntaxes, and where
// one route has a file in each the Blade page serves it.
//
// It returns the resolved entry and whether a serving page (or the 404 page)
// exists. A leading/trailing slash is ignored; an empty path is the index. A
// URL that would escape the pages root resolves to the 404 page (or nothing).
//
// v1 does NOT route automatically — hosts drive Compile with explicit protocol
// entries — but this pins the table the later Folio-mount phase must match, and
// is the engine-side companion to the oracle-checked routing-table fixture
// (which pins the entry→output-name half the URL table can't express).
func ResolveURL(fs vfs.FS, urlPath string) (string, bool) {
	seg := strings.Trim(urlPath, "/")
	if seg == "" {
		seg = "index"
	}

	// Never let a crafted URL climb out of pages/. path.Clean collapses the
	// segment; if it still points at or above the parent, refuse to resolve a
	// flat/nested page and fall through to the 404 page only.
	if clean := path.Clean(seg); clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return notFoundOr(fs)
	}

	// A flat file wins over a directory index in either syntax; within one
	// shape the Blade page wins (pageExts order).
	for _, ext := range pageExts {
		if flat := pagesPrefix + seg + ext; exists(fs, flat) {
			return flat, true
		}
	}
	for _, ext := range pageExts {
		if nested := pagesPrefix + seg + "/index" + ext; exists(fs, nested) {
			return nested, true
		}
	}
	return notFoundOr(fs)
}

// notFoundOr returns the 404 page entry when present, else ("", false).
func notFoundOr(fs vfs.FS) (string, bool) {
	for _, ext := range pageExts {
		if nf := pagesPrefix + "404" + ext; exists(fs, nf) {
			return nf, true
		}
	}
	return "", false
}

func exists(fs vfs.FS, p string) bool {
	_, ok := fs.Read(p)
	return ok
}
