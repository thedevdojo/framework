// Package site answers one question for the whole engine: where does this site
// keep its files?
//
// A site's source folder is src/, laid out flat:
//
//	src/pages/         one file per page
//	src/components/    components, with layouts/ and sections/ inside
//	src/data/          site.json, collections/, content/
//	src/css/           stylesheets
//
// Two older shapes are still read, so a site written before this one builds as
// it did: the source folder may be resources/ instead of src/, and pages and
// components may each sit one level down, under views/. All four combinations
// work (src/pages, src/views/pages, resources/pages, resources/views/pages).
//
// The answer is worked out once per compile, by Detect, from the files the
// site has. It is never asked file by file: a site has ONE source folder, ONE
// pages folder and ONE components folder. Where two candidates both hold
// files, the newer one is used, the other is not read at all, and a notice
// says so, because half a site in each is how a page renders blank with no
// error.
//
// Nothing else in the engine spells one of these folder names.
package site

import (
	"strconv"
	"strings"

	"github.com/thedevdojo/framework/vfs"
)

// The two source folders, newest first.
const (
	Src       = "src/"
	Resources = "resources/"
)

// views is the folder pages and components used to sit under.
const views = "views/"

// Notice is something a site's author should know that is not a problem with
// the site: it builds, and it will keep building. Notices never travel with
// diagnostics, because a host may treat any diagnostic as a failure.
type Notice struct {
	Kind    string `json:"kind"`           // always "deprecation" today
	Code    string `json:"code"`           // stable, for a host to switch on
	File    string `json:"file,omitempty"` // the file to change, when there is one
	Message string `json:"message"`
}

// KindDeprecation marks a notice about something that still works and has a
// newer way of being written.
const KindDeprecation = "deprecation"

// Notice codes.
const (
	CodeResourcesFolder = "resources-folder" // the source folder is resources/
	CodeViewsFolder     = "views-folder"     // pages or components are under views/
	CodeFolderNotRead   = "folder-not-read"  // two candidates hold files; one is ignored
	CodeAssetPath       = "asset-path"       // an asset is named in the folder the site does not use
	CodeDoubleBraces    = "double-braces"    // an .html view prints values with {{ }}, not { }
)

// Layout is where one site keeps its files. Every path ends in a slash.
type Layout struct {
	Root       string // the source folder: "src/" or "resources/"
	Pages      string // e.g. "src/pages/"
	Components string // e.g. "src/components/"
	Notices    []Notice
}

// Default is the layout of a site with no files yet: flat, under src/.
func Default() Layout {
	return Layout{Root: Src, Pages: Src + "pages/", Components: Src + "components/"}
}

// Detect works out a site's layout from the files it has.
func Detect(fs vfs.FS) Layout {
	has := func(prefix string) bool { return len(fs.List(prefix)) > 0 }

	l := Layout{Root: Src}
	switch {
	case has(Src):
		if has(Resources) {
			l.Notices = append(l.Notices, notRead(Resources, Src))
		}
	case has(Resources):
		l.Root = Resources
		l.Notices = append(l.Notices, Notice{
			Kind: KindDeprecation,
			Code: CodeResourcesFolder,
			Message: "This site keeps its source in resources/. That is the old name for the folder: " +
				"the new one is src/, with pages in src/pages and components in src/components. " +
				"Run `devdojo migrate` to move it.",
		})
	}

	// Each of the two view folders is found on its own: flat first, then
	// under views/. One that has no files yet follows the other, so a site
	// with pages and no components looks for a component beside its pages.
	// With neither, src/ is flat and resources/ keeps the shape it always had.
	var underViews []string
	found := map[string]bool{} // name → it was found under views/
	pick := func(name string) (path string, ok bool) {
		flat, old := l.Root+name, l.Root+views+name
		switch {
		case has(flat):
			if has(old) {
				l.Notices = append(l.Notices, notRead(old, flat))
			}
			found[name] = false
			return flat, true
		case has(old):
			underViews = append(underViews, old)
			found[name] = true
			return old, true
		}
		return "", false
	}
	const pages, components = "pages/", "components/"
	var havePages, haveComponents bool
	l.Pages, havePages = pick(pages)
	l.Components, haveComponents = pick(components)
	under := func(name string) string {
		other := components
		if name == components {
			other = pages
		}
		nested, known := found[other]
		if !known {
			nested = l.Root == Resources
		}
		if nested {
			return l.Root + views + name
		}
		return l.Root + name
	}
	if !havePages {
		l.Pages = under(pages)
	}
	if !haveComponents {
		l.Components = under(components)
	}

	// A resources/ site has already been told its whole shape is old; only a
	// src/ site needs telling about views/ on its own.
	if len(underViews) > 0 && l.Root == Src {
		l.Notices = append(l.Notices, Notice{
			Kind: KindDeprecation,
			Code: CodeViewsFolder,
			Message: "This site keeps " + strings.Join(underViews, " and ") + " under views/. " +
				"Pages now live in src/pages and components in src/components. " +
				"Run `devdojo migrate` to move them.",
		})
	}
	return l
}

// notRead is the notice for a folder that holds files and is being ignored
// because a newer one does too.
func notRead(ignored, used string) Notice {
	return Notice{
		Kind: KindDeprecation,
		Code: CodeFolderNotRead,
		Message: "This site has both " + used + " and " + ignored + ". Only " + used +
			" is read: move what is still needed out of " + ignored + " and delete it.",
	}
}

// SiteData is the path of the site's global data object.
func (l Layout) SiteData() string { return l.Root + "data/site.json" }

// Collections is the folder of named lists.
func (l Layout) Collections() string { return l.Root + "data/collections/" }

// Content is the folder of markdown collections.
func (l Layout) Content() string { return l.Root + "data/content/" }

// Asset maps a path a template names (a <Vite> entry) onto this site's source
// folder. A site moved from resources/ to src/ still says
// `resources/css/site.css` in its layout until that line is changed; the file
// is found at src/css/site.css either way. moved reports that the path named
// the other folder.
func (l Layout) Asset(p string) (resolved string, moved bool) {
	other := Resources
	if l.Root == Resources {
		other = Src
	}
	if strings.HasPrefix(p, other) {
		return l.Root + p[len(other):], true
	}
	return p, false
}

// AssetNotice is the notice for an asset named in the folder a site does not
// use.
func AssetNotice(file, named, resolved string) Notice {
	return Notice{
		Kind:    KindDeprecation,
		Code:    CodeAssetPath,
		File:    file,
		Message: "\"" + named + "\" was read from " + resolved + ". Change the path in " + file + " to match.",
	}
}

// DoubleBracesNotice is the notice for an .html view that writes count values
// in double braces, the older spelling of a single brace.
func DoubleBracesNotice(file string, count int) Notice {
	what, it := "a value in double braces, {{ x }}. It still works", "it"
	if count != 1 {
		what, it = strconv.Itoa(count)+" values in double braces, {{ x }}. They still work", "them"
	}
	return Notice{
		Kind:    KindDeprecation,
		Code:    CodeDoubleBraces,
		File:    file,
		Message: file + " writes " + what + ", but a value is now written in single braces, {x}. Run `devdojo migrate` to rewrite " + it + ".",
	}
}

// pageFolders and viewFolders are every place a page, or any other view, may
// be, most specific first. OutputName does not depend on which of them a site
// uses, so an entry a host names keeps its output name whatever else the site
// holds.
var (
	pageFolders = []string{Src + "pages/", Src + views + "pages/", Resources + "pages/", Resources + views + "pages/"}
	viewFolders = []string{Src + views, Resources + views, Src, Resources}
)

// OutputName maps an entry path to its output file name. A page loses its
// pages folder; any other view keeps its path from the folder that holds
// pages/ and components/. A Blade file's .blade.php becomes .html; an .html
// file keeps it. So src/pages/about.html and
// resources/views/pages/about.blade.php both render to about.html.
func OutputName(entry string) string {
	name := entry
	trimmed := false
	for _, folder := range pageFolders {
		if strings.HasPrefix(name, folder) {
			name, trimmed = name[len(folder):], true
			break
		}
	}
	if !trimmed {
		for _, folder := range viewFolders {
			if strings.HasPrefix(name, folder) {
				name = name[len(folder):]
				break
			}
		}
	}
	if strings.HasSuffix(name, ".blade.php") {
		name = strings.TrimSuffix(name, ".blade.php")
	} else {
		name = strings.TrimSuffix(name, ".html")
	}
	return name + ".html"
}
