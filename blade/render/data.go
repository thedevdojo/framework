package render

import (
	"sort"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/content"
	"github.com/thedevdojo/pocketknife/vfs"
)

// Data-source layout under the site root. site.json is a global object; every
// <name>.json under collections/ — or every <name>/ folder of one JSON object
// per entry — is a named array binding; every <dir>/*.md under content/ is a
// named collection of rendered markdown items.
const (
	siteDataPath   = "resources/data/site.json"
	collectionsDir = "resources/data/collections/"
	contentDir     = "resources/data/content/"
)

// siteData is the assembled data context for one Compile: the global $site
// object, the GLOBAL shared bindings (collections + content), and the
// page-level bindings (site + collections + content, with Options.Data merged
// over the top, keys winning).
type siteData struct {
	site expr.Value // $site — the site.json object (empty map if absent)
	// shared holds the collection and content bindings exactly as loaded from
	// disk — no Options.Data merged over them and no `site` key. Since
	// 2026-07-28 these bind GLOBALLY (spec
	// docs/specs/2026-07-28-collections-global-scope-design.md): components and
	// layouts see them, like $site, mirroring real Blade's View::share. Page
	// scope keeps using `bindings` so a dynamic page's Options.Data entry
	// override stays page-local.
	shared   map[string]expr.Value
	bindings map[string]expr.Value // page top-level variables
	diags    []Diagnostic
}

// buildSiteData reads the three data sources under the site root and folds
// Options.Data over them. Each collection binds first, then each content
// collection, then $site, then the caller's data (which overrides collisions —
// a data key `site` therefore replaces $site for pages). The site-after-
// collections order mirrors SiteData::share, which shares `site` LAST so a
// collection literally named `site` cannot displace the site object anywhere.
// The returned `site` is the ORIGINAL site.json object, and `shared` the
// ORIGINAL collection/content bindings, both unaffected by any data override,
// because components see the SHARED data, not per-page data (parity with real
// Blade's View::share vs. per-view data).
func buildSiteData(fs vfs.FS, data map[string]any) siteData {
	site, diags := loadSite(fs)

	bindings := map[string]expr.Value{}
	shared := map[string]expr.Value{}

	colBindings, colDiags := loadCollections(fs)
	diags = append(diags, colDiags...)
	for name, v := range colBindings {
		bindings[name] = v
		shared[name] = v
	}

	contentBindings, conDiags := loadContent(fs)
	diags = append(diags, conDiags...)
	for name, v := range contentBindings {
		bindings[name] = v
		shared[name] = v
	}

	bindings["site"] = site

	// Options.Data merges over everything; its keys become top-level variables
	// and win on collision (including `site`) because real Blade's per-view data
	// outranks all shared data. It is deliberately NOT folded into `shared`: the
	// protocol's data map is per-page view data, which never reaches an
	// anonymous component's isolated scope.
	for k, v := range data {
		bindings[k] = toValue(v)
	}

	return siteData{site: site, shared: shared, bindings: bindings, diags: diags}
}

// loadSite reads resources/data/site.json into the $site object. A missing file
// yields an empty object and no diagnostic; malformed JSON (or a non-object, e.g.
// a top-level array) yields an empty object and one site-data error. The
// order-preserving decoder keeps $site's keys in document order so foreach over
// $site matches real Blade's stdClass insertion-order iteration.
func loadSite(fs vfs.FS) (expr.Value, []Diagnostic) {
	raw, ok := fs.Read(siteDataPath)
	if !ok {
		return expr.NewObject(), nil
	}
	val, err := expr.DecodeJSON(raw)
	if err != nil {
		return expr.NewObject(), []Diagnostic{dataDiag(siteDataPath, "site-data", "invalid site.json: "+err.Error())}
	}
	obj, ok := val.(*expr.Object)
	if !ok {
		return expr.NewObject(), []Diagnostic{dataDiag(siteDataPath, "site-data", "site.json must be a JSON object")}
	}
	return obj, nil
}

// collectionSource is everything found under collections/ for one name: the
// single-file form, the folder form, or (an authoring mistake) both.
type collectionSource struct {
	single string   // collections/<name>.json, "" when absent
	folder []string // collections/<name>/<file>.json, sorted by path
}

// loadCollections reads every collection under resources/data/collections/
// into a $<name> array binding. A collection takes one of two storage forms:
//
//   - a single file, collections/<name>.json — a JSON array, bound in file
//     order (the default);
//   - a folder, collections/<name>/<slug>.json — one JSON OBJECT per entry,
//     the file name equal to the entry's slug, bound by `date` descending and
//     then file name ascending. The form for a collection past a hundred
//     entries, where one array file would outgrow a host's per-file limit.
//
// Both bind the same $<name>; templates cannot tell the forms apart. When a
// site carries both for one name the folder wins, the single file is ignored
// entirely (never merged), and a collection error names both paths so the
// author removes one. The name must be a valid PHP variable name; a single
// file must decode to a JSON array, a folder entry to a JSON object — either
// failure is a collection error and that file is skipped. A folder entry
// whose `slug` differs from its file name still binds, with a collection
// error, because the file name is the entry's address for every host tool.
// Only direct children are read: a deeper collections/<name>/<dir>/x.json is
// ignored, as before.
func loadCollections(fs vfs.FS) (map[string]expr.Value, []Diagnostic) {
	sources := map[string]*collectionSource{}
	var names []string
	for _, path := range fs.List(collectionsDir) {
		if !strings.HasPrefix(path, collectionsDir) || !strings.HasSuffix(path, ".json") {
			continue
		}
		parts := strings.Split(path[len(collectionsDir):], "/")
		var name string
		switch len(parts) {
		case 1:
			name = strings.TrimSuffix(parts[0], ".json")
		case 2:
			name = parts[0]
		default:
			continue
		}
		src, seen := sources[name]
		if !seen {
			src = &collectionSource{}
			sources[name] = src
			names = append(names, name)
		}
		if len(parts) == 1 {
			src.single = path
		} else {
			src.folder = append(src.folder, path)
		}
	}
	sort.Strings(names)

	out := map[string]expr.Value{}
	var diags []Diagnostic
	for _, name := range names {
		src := sources[name]
		if !isValidVarName(name) {
			file := src.single
			if file == "" {
				file = collectionsDir + name
			}
			diags = append(diags, dataDiag(file, "collection", "collection name \""+name+"\" is not a valid variable name"))
			continue
		}
		if len(src.folder) == 0 {
			if arr, ok := loadCollectionFile(fs, name, src.single, &diags); ok {
				out[name] = arr
			}
			continue
		}
		if src.single != "" {
			diags = append(diags, dataDiag(src.single, "collection", "collection \""+name+"\" is stored twice: "+src.single+" and "+collectionsDir+name+"/ — the folder is used and the file is ignored; remove one of them"))
		}
		out[name] = loadCollectionFolder(fs, name, src.folder, &diags)
	}
	return out, diags
}

// loadCollectionFile decodes the single-file form: the file must be a JSON
// array, whose object ITEMS the order-preserving decoder turns into *Object
// values (stdClass semantics), nested objects too, while the array itself
// stays an ordered []Value.
func loadCollectionFile(fs vfs.FS, name, path string, diags *[]Diagnostic) ([]expr.Value, bool) {
	raw, ok := fs.Read(path)
	if !ok {
		return nil, false
	}
	val, err := expr.DecodeJSON(raw)
	if err != nil {
		*diags = append(*diags, dataDiag(path, "collection", "collection \""+name+"\" must be a JSON array"))
		return nil, false
	}
	arr, ok := val.([]expr.Value)
	if !ok {
		*diags = append(*diags, dataDiag(path, "collection", "collection \""+name+"\" must be a JSON array"))
		return nil, false
	}
	return arr, true
}

// folderEntry is one decoded file of the folder form, with the keys its
// position in the binding is decided by.
type folderEntry struct {
	file  string // the file name minus .json — the entry's slug
	date  string // the entry's `date` when it is a string, "" otherwise
	value *expr.Object
}

// loadCollectionFolder decodes the folder form: every direct collections/
// <name>/<file>.json must be a JSON object (anything else is a collection
// error and that file is skipped). Entries bind by `date` descending — the
// newest first, the way a listing wants them — and then file name ascending,
// so two entries on one day keep a stable order; an entry without a string
// `date` sorts after every dated one.
func loadCollectionFolder(fs vfs.FS, name string, paths []string, diags *[]Diagnostic) []expr.Value {
	entries := make([]folderEntry, 0, len(paths))
	for _, path := range paths {
		raw, ok := fs.Read(path)
		if !ok {
			continue
		}
		file := strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".json")
		val, err := expr.DecodeJSON(raw)
		if err != nil {
			*diags = append(*diags, dataDiag(path, "collection", "collection \""+name+"\" entry "+file+".json must be a JSON object"))
			continue
		}
		obj, ok := val.(*expr.Object)
		if !ok {
			*diags = append(*diags, dataDiag(path, "collection", "collection \""+name+"\" entry "+file+".json must be a JSON object"))
			continue
		}
		if slug, ok := obj.Get("slug"); ok {
			if s, isString := slug.(string); isString && s != file {
				*diags = append(*diags, dataDiag(path, "collection", "collection \""+name+"\" entry "+file+".json has slug \""+s+"\" — name the file after the entry's slug"))
			}
		}
		entry := folderEntry{file: file, value: obj}
		if date, ok := obj.Get("date"); ok {
			if s, isString := date.(string); isString {
				entry.date = s
			}
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].date != entries[j].date {
			return entries[i].date > entries[j].date
		}
		return entries[i].file < entries[j].file
	})
	out := make([]expr.Value, len(entries))
	for i, e := range entries {
		out[i] = e.value
	}
	return out
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

// contentItemValue turns a rendered content Item into the *Object an expression
// sees: the frontmatter fields in document order, then `content` (rendered HTML)
// and `link` (route). content/link are added last, so they override any
// frontmatter key of the same name — and, being an object, a missing member is an
// error while a present one reads, mirroring real Blade's (object)$fields item.
func contentItemValue(it content.Item) expr.Value {
	obj := expr.NewObject()
	for _, k := range it.Order {
		obj.Set(k, toValue(it.Fields[k]))
	}
	obj.Set("content", it.Content)
	obj.Set("link", it.Link)
	return obj
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
