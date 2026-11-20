package main

import (
	"encoding/json"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/thedevdojo/framework/protocol"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

// The routing rules `serve` and `build` share: which page answers an
// address, and which addresses a dynamic page answers. They are the
// platform's rules (protocol.ResolveURL for static pages, DynamicPages.php
// for dynamic ones), kept in one place so a built site has exactly the pages
// `serve` showed.

// servePageExts are the two page extensions, Blade first: where one address
// has a page in each syntax the Blade one serves it (protocol/routing.go).
var servePageExts = []string{".blade.php", ".html"}

// resolvePage finds the page for a URL, in the order the platform serves a
// published site: a static page, then a dynamic one, then the site's 404
// page with status 404. found is false when there is not even a 404 page.
func resolvePage(files vfs.FS, urlPath string, safe bool) (target pageTarget, status int, found bool) {
	layout := site.Detect(files)
	notFound := ""
	for _, ext := range servePageExts {
		if _, ok := files.Read(layout.Pages + "404" + ext); ok {
			notFound = layout.Pages + "404" + ext
			break
		}
	}
	if safe {
		// ResolveURL answers with the 404 page for an address it has no page
		// for. That is only a hit when the 404 page is what was asked for. A
		// dynamic page's own file name is not an address either: compiled
		// with no entry bound it could only fail.
		entry, ok := protocol.ResolveURL(files, urlPath)
		direct := entry != notFound || strings.Trim(urlPath, "/") == "404"
		if _, _, dynamic := parseDynamicPage(entry); ok && direct && !dynamic {
			return pageTarget{entry: entry}, http.StatusOK, true
		}
		if target, ok := resolveDynamicPage(files, layout, urlPath); ok {
			return target, http.StatusOK, true
		}
	}
	if notFound == "" {
		return pageTarget{}, http.StatusNotFound, false
	}
	return pageTarget{entry: notFound}, http.StatusNotFound, true
}

// dynamicPageName is the `[collection.field]` file name of a dynamic page.
// Both halves follow the variable-name rule the engine applies to a
// collection's name. It is the platform's own pattern (DynamicPages.php).
var dynamicPageName = regexp.MustCompile(`^\[([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\]$`)

// parseDynamicPage reads the collection and field out of a dynamic page's
// path, e.g. src/pages/blog/[posts.slug].html → posts, slug.
func parseDynamicPage(pagePath string) (collection, field string, ok bool) {
	base := path.Base(pagePath)
	for _, ext := range servePageExts {
		if !strings.HasSuffix(base, ext) {
			continue
		}
		if m := dynamicPageName.FindStringSubmatch(strings.TrimSuffix(base, ext)); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// resolveDynamicPage finds the dynamic page and the collection entry a URL
// names. The URL's last segment is the value looked for; the segments before
// it name the folder that holds the page. The first dynamic page in that
// folder, in name order, whose collection has a matching entry serves it.
//
// This is the platform's lookup (DynamicPages::resolveUrl) and binds what the
// platform binds (FrameworkCompiler::dynamicResult): the collection's name is
// the ONE matched entry for this render, and `entries` is the whole
// collection, for a page that also lists the others.
func resolveDynamicPage(files vfs.FS, layout site.Layout, urlPath string) (pageTarget, bool) {
	p := strings.Trim(urlPath, "/")
	if p == "" || strings.Contains(p, "..") {
		return pageTarget{}, false
	}
	cut := strings.LastIndex(p, "/") + 1
	directory, segment := layout.Pages+p[:cut], p[cut:]

	for _, candidate := range files.List(directory) {
		// Direct children only: a dynamic page in a folder further down
		// serves its own, deeper addresses.
		if path.Dir(candidate)+"/" != directory {
			continue
		}
		collection, field, ok := parseDynamicPage(candidate)
		if !ok {
			continue
		}
		// A Studio page with a Blade twin beside it is never served.
		if strings.HasSuffix(candidate, ".html") {
			if _, twin := files.Read(strings.TrimSuffix(candidate, ".html") + ".blade.php"); twin {
				continue
			}
		}
		entries := collectionEntries(files, layout, collection)
		for _, entry := range entries {
			if value, ok := scalarString(entry.(map[string]any)[field]); ok && value == segment {
				return pageTarget{entry: candidate, data: map[string]any{collection: entry, "entries": entries}}, true
			}
		}
	}
	return pageTarget{}, false
}

// collectionEntries is a collection's entries: data/collections/<name>.json
// read as a JSON array, keeping the objects in it. A file that is missing,
// malformed or not an array has none; the engine says what is wrong with it
// when a page reads it. Every element is a map[string]any; the slice is
// []any because that is the shape render.Options.Data takes.
func collectionEntries(files vfs.FS, layout site.Layout, name string) []any {
	raw, ok := files.Read(layout.Collections() + name + ".json")
	if !ok {
		return nil
	}
	var list []any
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	entries := make([]any, 0, len(list))
	for _, item := range list {
		if _, isObject := item.(map[string]any); isObject {
			entries = append(entries, item)
		}
	}
	return entries
}

// scalarString is a field's value as the text a URL would carry, the way the
// platform compares them (PHP's string cast): a number as its digits, true as
// "1". Anything that is not a single value never matches an address.
func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		if x {
			return "1", true
		}
		return "", true
	}
	return "", false
}

// dynamicPageURLs is every address a dynamic page could answer, without a
// leading slash: the folder it is in, then one entry's value of the field it
// is named for. A value that is empty, not a single value, or that has a `/`
// or `..` in it is no address (the platform's DynamicPages::urlsFor). Which
// page really answers each one is still resolvePage's to say.
func dynamicPageURLs(files vfs.FS, layout site.Layout, page string) []string {
	collection, field, ok := parseDynamicPage(page)
	if !ok {
		return nil
	}
	folder := strings.TrimPrefix(path.Dir(page)+"/", layout.Pages)
	seen := map[string]bool{}
	var urls []string
	for _, entry := range collectionEntries(files, layout, collection) {
		value, ok := scalarString(entry.(map[string]any)[field])
		if !ok || value == "" || strings.Contains(value, "/") || strings.Contains(value, "..") {
			continue
		}
		url := folder + value
		if !seen[url] {
			seen[url] = true
			urls = append(urls, url)
		}
	}
	return urls
}

// dynamicPagePreview is what a dynamic page is compiled with when it is
// asked for by its own file name rather than an address (`devdojo lint`): the
// first entry of its collection bound under the collection's name, and the
// whole collection as `entries`, as the platform binds it to preview the
// page. ok is false for a page that is not dynamic.
func dynamicPagePreview(files vfs.FS, layout site.Layout, page string) (map[string]any, bool) {
	collection, _, ok := parseDynamicPage(page)
	if !ok {
		return nil, false
	}
	entries := collectionEntries(files, layout, collection)
	if entries == nil {
		entries = []any{}
	}
	data := map[string]any{"entries": entries}
	if len(entries) > 0 {
		data[collection] = entries[0]
	}
	return data, true
}
