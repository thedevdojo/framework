// Package migrate moves a site written in one of the older folder shapes into
// the current one: src/, with pages in src/pages and components in
// src/components (see package site).
//
// Like a conversion between the two syntaxes, a move is exact or it does not
// happen. The site is rendered before and after, every page must come out byte
// for byte the same, and one difference leaves every file where it was.
package migrate

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

// Move is one file changing its path.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Result is a migration: the files that move and, for the few whose text names
// an old path, their new contents. When Problems is not empty the site was not
// moved and both are empty.
type Result struct {
	Moves    []Move              // sorted by From
	Files    map[string]string   // new path → new contents, only where the text changed
	Problems []render.Diagnostic // why the site could not be moved
}

// Site works out the move for the site in fs. opts carries the data the
// before-and-after render check runs with: pass what the site is normally
// rendered with, so the check exercises the same branches.
//
// A site already in the current shape returns an empty Result.
func Site(fs vfs.FS, opts render.Options) Result {
	res := Result{Files: map[string]string{}}
	from, to := site.Detect(fs), site.Default()

	target := func(p string) string {
		switch {
		case strings.HasPrefix(p, from.Pages):
			return to.Pages + p[len(from.Pages):]
		case strings.HasPrefix(p, from.Components):
			return to.Components + p[len(from.Components):]
		}
		return to.Root + p[len(from.Root):]
	}

	moving := map[string]string{}
	for _, p := range fs.List(from.Root) {
		if dest := target(p); dest != p {
			moving[p] = dest
			res.Moves = append(res.Moves, Move{From: p, To: dest})
		}
	}
	if len(res.Moves) == 0 {
		return res
	}
	sort.Slice(res.Moves, func(i, j int) bool { return res.Moves[i].From < res.Moves[j].From })

	for _, m := range res.Moves {
		if _, taken := fs.Read(m.To); taken {
			if _, alsoMoving := moving[m.To]; !alsoMoving {
				res.Problems = append(res.Problems, problem(m.To, "already exists; "+m.From+" cannot be moved onto it"))
			}
		}
	}

	rewrite := rewriter(from, to)
	for _, m := range res.Moves {
		kind := textKind(m.From, from)
		if kind == notText {
			continue
		}
		src, ok := fs.Read(m.From)
		if !ok {
			continue
		}
		if out := rewrite(kind, string(src)); out != string(src) {
			res.Files[m.To] = out
		}
	}

	if len(res.Problems) == 0 {
		pages := pagesOf(fs, from.Pages)
		before := render.Compile(fs, pages, opts)
		moved := make([]string, len(pages))
		for i, p := range pages {
			moved[i] = target(p)
		}
		after := render.Compile(overlay{base: fs, moving: moving, files: res.Files}, moved, opts)
		for i, page := range pages {
			name := site.OutputName(page)
			if line, same := firstDifference(before.Files[name], after.Files[site.OutputName(moved[i])]); !same {
				res.Problems = append(res.Problems, problem(page,
					"would render differently after moving (output line "+strconv.Itoa(line)+"); nothing was changed"))
			}
		}
		if len(res.Problems) == 0 && len(after.Diagnostics) != len(before.Diagnostics) {
			res.Problems = append(res.Problems, problem(from.Root,
				"the site reports "+strconv.Itoa(len(after.Diagnostics))+" problems after moving and "+
					strconv.Itoa(len(before.Diagnostics))+" before; nothing was changed"))
		}
	}

	if len(res.Problems) > 0 {
		res.Moves, res.Files = nil, map[string]string{}
	}
	return res
}

// Apply carries the migration out on an in-memory site and returns the result.
// The files it was given are not changed.
func (r Result) Apply(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for p, body := range files {
		out[p] = body
	}
	for _, m := range r.Moves {
		body, ok := out[m.From]
		if !ok {
			continue
		}
		delete(out, m.From)
		out[m.To] = body
	}
	for p, body := range r.Files {
		out[p] = body
	}
	return out
}

func problem(file, message string) render.Diagnostic {
	return render.Diagnostic{File: file, Kind: render.KindError, Construct: "migrate", Message: message}
}

// pagesOf lists every page under a pages folder, in either syntax, sorted.
func pagesOf(fs vfs.FS, pages string) []string {
	var out []string
	for _, p := range fs.List(pages) {
		if strings.HasSuffix(p, ".blade.php") || strings.HasSuffix(p, ".html") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// What a file's text is, for deciding what in it may be rewritten.
const (
	notText   = iota
	bladeView // a .blade.php page or component: @vite(...) names a stylesheet
	htmlView  // an .html page or component: <Vite src="..."> names one
	notes     // a component's .yml or a note about the site: prose, never rendered
)

// textKind sorts a file by what in it may name an old path. Everything not
// listed here moves untouched: data, content, stylesheets, scripts, pictures.
// Markdown under data/ is the site's CONTENT and is printed on a page, so a
// path a writer typed there stays exactly as they typed it.
func textKind(p string, l site.Layout) int {
	view := strings.HasPrefix(p, l.Pages) || strings.HasPrefix(p, l.Components)
	switch {
	case view && strings.HasSuffix(p, ".blade.php"):
		return bladeView
	case view && strings.HasSuffix(p, ".html"):
		return htmlView
	case view && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")):
		return notes
	case strings.HasSuffix(p, ".md") && !strings.HasPrefix(p, l.Root+"data/"):
		return notes
	}
	return notText
}

var (
	bladeVite = regexp.MustCompile(`@vite\([^)]*\)`)
	htmlVite  = regexp.MustCompile(`<Vite\b[^>]*>`)
)

// rewriter returns the function that brings a file's text up to date with the
// move. In a view only a stylesheet entry is touched, in the one spelling the
// file's own syntax reads, because that is the only path in a view the engine
// resolves; anything else in a view is either printed or ignored, and is left
// alone. Notes are prose and every old path in them is rewritten.
func rewriter(from, to site.Layout) func(kind int, text string) string {
	quoted := func(s string) string {
		if from.Root == to.Root {
			return s
		}
		s = strings.ReplaceAll(s, `'`+from.Root, `'`+to.Root)
		return strings.ReplaceAll(s, `"`+from.Root, `"`+to.Root)
	}

	// Old path → new path, longest first, without the trailing slash so
	// "in resources/views/pages." is caught as well as "resources/views/pages/x".
	type pair struct{ old, new string }
	var pairs []pair
	add := func(old, new string) {
		old, new = strings.TrimSuffix(old, "/"), strings.TrimSuffix(new, "/")
		if old != new {
			pairs = append(pairs, pair{old, new})
		}
	}
	add(from.Pages, to.Pages)
	add(from.Components, to.Components)
	add(from.Root+"views/", to.Root)
	if from.Root != to.Root {
		pairs = append(pairs, pair{from.Root, to.Root}) // with its slash: never the bare word
	}
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].old) > len(pairs[j].old) })
	var prose []*regexp.Regexp
	for _, p := range pairs {
		// Not in the middle of a longer path or a URL.
		prose = append(prose, regexp.MustCompile(`(^|[^A-Za-z0-9_./-])`+regexp.QuoteMeta(p.old)))
	}

	return func(kind int, text string) string {
		switch kind {
		case bladeView:
			return bladeVite.ReplaceAllStringFunc(text, quoted)
		case htmlView:
			return htmlVite.ReplaceAllStringFunc(text, quoted)
		case notes:
			for i, re := range prose {
				text = re.ReplaceAllString(text, "${1}"+pairs[i].new)
			}
		}
		return text
	}
}

// overlay is the site as it will be after the move, read through the site as
// it is: a moved file answers at its new path only.
type overlay struct {
	base   vfs.FS
	moving map[string]string // old path → new path
	files  map[string]string // new path → rewritten contents
}

func (o overlay) Read(path string) ([]byte, bool) {
	if body, ok := o.files[path]; ok {
		return []byte(body), true
	}
	if _, gone := o.moving[path]; gone {
		return nil, false
	}
	for from, to := range o.moving {
		if to == path {
			return o.base.Read(from)
		}
	}
	return o.base.Read(path)
}

func (o overlay) List(prefix string) []string {
	var out []string
	for _, p := range o.base.List(prefix) {
		if _, gone := o.moving[p]; !gone {
			out = append(out, p)
		}
	}
	for _, to := range o.moving {
		if strings.HasPrefix(to, prefix) {
			out = append(out, to)
		}
	}
	sort.Strings(out)
	return out
}

// firstDifference reports the first line two renders differ on (1-based), and
// whether they are the same.
func firstDifference(a, b string) (int, bool) {
	if a == b {
		return 0, true
	}
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return i + 1, false
		}
	}
	if len(la) < len(lb) {
		return len(la) + 1, false
	}
	return len(lb) + 1, false
}
