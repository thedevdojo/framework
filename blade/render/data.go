package render

import (
	"encoding/json"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/content"
	"github.com/thedevdojo/pocketknife/vfs"
)

// Data-source layout under the site root. site.json is a global object; every
// *.json under collections/ is a named array binding; every <dir>/*.md under
// content/ is a named collection of rendered markdown items.
const (
	siteDataPath   = "resources/data/site.json"
	collectionsDir = "resources/data/collections/"
	contentDir     = "resources/data/content/"
)

// siteData is the assembled data context for one Compile: the global $site
// object (as seen by components too) and the page-level bindings (site +
// collections + content, with Options.Data merged over the top, keys winning).
type siteData struct {
	site     expr.Value            // $site — the site.json object (empty map if absent)
	bindings map[string]expr.Value // page top-level variables
	diags    []Diagnostic
}

// buildSiteData reads the three data sources under the site root and folds
// Options.Data over them. $site is bound first, then each collection, then each
// content collection, then the caller's data (which overrides collisions — a
// data key `site` therefore replaces $site for pages). The returned `site` is
// the ORIGINAL site.json object, unaffected by any data override, because
// components see the shared $site, not per-page data (parity with real Blade's
// View::share vs. per-view data).
func buildSiteData(fs vfs.FS, data map[string]any) siteData {
	site, diags := loadSite(fs)

	bindings := map[string]expr.Value{"site": site}

	colBindings, colDiags := loadCollections(fs)
	diags = append(diags, colDiags...)
	for name, v := range colBindings {
		bindings[name] = v
	}

	contentBindings, conDiags := loadContent(fs)
	diags = append(diags, conDiags...)
	for name, v := range contentBindings {
		bindings[name] = v
	}

	// Options.Data merges over everything; its keys become top-level variables
	// and win on collision (including `site`).
	for k, v := range data {
		bindings[k] = toValue(v)
	}

	return siteData{site: site, bindings: bindings, diags: diags}
}

// loadSite reads resources/data/site.json into the $site object. A missing file
// yields an empty object and no diagnostic; malformed JSON (or a non-object, e.g.
// a top-level array) yields an empty object and one site-data error.
func loadSite(fs vfs.FS) (expr.Value, []Diagnostic) {
	raw, ok := fs.Read(siteDataPath)
	if !ok {
		return map[string]expr.Value{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return map[string]expr.Value{}, []Diagnostic{dataDiag(siteDataPath, "site-data", "invalid site.json: "+err.Error())}
	}
	m := make(map[string]expr.Value, len(obj))
	for k, v := range obj {
		m[k] = toValue(v)
	}
	return m, nil
}

// loadCollections reads every resources/data/collections/<name>.json into a
// $<name> array binding. The basename (minus .json) must be a valid PHP variable
// name and the file must decode to a JSON array; either failure is a collection
// error and the binding is skipped.
func loadCollections(fs vfs.FS) (map[string]expr.Value, []Diagnostic) {
	out := map[string]expr.Value{}
	var diags []Diagnostic
	for _, path := range fs.List(collectionsDir) {
		rest, ok := directChild(collectionsDir, path)
		if !ok || !strings.HasSuffix(rest, ".json") {
			continue
		}
		name := strings.TrimSuffix(rest, ".json")
		if !isValidVarName(name) {
			diags = append(diags, dataDiag(path, "collection", "collection name \""+name+"\" is not a valid variable name"))
			continue
		}
		raw, ok := fs.Read(path)
		if !ok {
			continue
		}
		var arr []any
		if err := json.Unmarshal(raw, &arr); err != nil {
			diags = append(diags, dataDiag(path, "collection", "collection \""+name+"\" must be a JSON array"))
			continue
		}
		out[name] = toValue(arr)
	}
	return out, diags
}

// loadContent reads every resources/data/content/<dir>/*.md into a $<dir> array
// of rendered items. The directory name must be a valid PHP variable name; a
// file whose frontmatter is non-scalar is skipped with a content error. Items
// are ordered by filename ascending.
func loadContent(fs vfs.FS) (map[string]expr.Value, []Diagnostic) {
	out := map[string]expr.Value{}
	var diags []Diagnostic

	dirs, order := groupContentFiles(fs)
	for _, dir := range order {
		if !isValidVarName(dir) {
			diags = append(diags, dataDiag(contentDir+dir, "content", "content directory \""+dir+"\" is not a valid variable name"))
			continue
		}
		items, errs := content.RenderCollection(dir, dirs[dir])
		for _, e := range errs {
			diags = append(diags, dataDiag(contentDir+dir+"/"+e.Name, "content", "content item \""+e.Name+"\": "+e.Err.Error()))
		}
		vals := make([]expr.Value, len(items))
		for i, it := range items {
			vals[i] = contentItemValue(it)
		}
		out[dir] = vals
	}
	return out, diags
}

// contentItemValue turns a rendered content Item into the map an expression
// sees: the frontmatter fields, then `content` (rendered HTML) and `link`
// (route). content/link are added last, so they override any frontmatter key of
// the same name.
func contentItemValue(it content.Item) expr.Value {
	m := make(map[string]expr.Value, len(it.Fields)+2)
	for k, v := range it.Fields {
		m[k] = toValue(v)
	}
	m["content"] = it.Content
	m["link"] = it.Link
	return m
}

// groupContentFiles lists content markdown files grouped by their immediate
// directory (one level under content/), returning the groups and the directory
// names in ascending order. Only direct <dir>/<name>.md files are collected;
// deeper nesting is ignored (v1 content is flat within a collection).
func groupContentFiles(fs vfs.FS) (map[string][]content.File, []string) {
	groups := map[string][]content.File{}
	var order []string
	for _, path := range fs.List(contentDir) {
		if !strings.HasPrefix(path, contentDir) {
			continue
		}
		rest := path[len(contentDir):]
		parts := strings.Split(rest, "/")
		if len(parts) != 2 || !strings.HasSuffix(parts[1], ".md") {
			continue
		}
		dir, name := parts[0], parts[1]
		raw, ok := fs.Read(path)
		if !ok {
			continue
		}
		if _, seen := groups[dir]; !seen {
			order = append(order, dir)
		}
		groups[dir] = append(groups[dir], content.File{Name: name, Data: raw})
	}
	return groups, order
}

// directChild reports the path segment immediately under prefix, and false when
// path is not under prefix or lies in a deeper subdirectory.
func directChild(prefix, path string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return rest, true
}

// isValidVarName reports whether name matches [a-zA-Z_][a-zA-Z0-9_]* — the PHP
// variable-name rule a collection/content binding must satisfy to become $name.
func isValidVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// dataDiag builds a file-level data diagnostic. Data errors are about a whole
// file rather than a position within it, so Line/Col are 0.
func dataDiag(file, construct, msg string) Diagnostic {
	return Diagnostic{File: file, Kind: KindError, Construct: construct, Message: msg}
}
