// Package convert rewrites a site from one of DevDojo Framework's two syntaxes into
// the other: Blade (.blade.php) to the DevDojo Studio syntax (.html), and back.
//
// The two syntaxes are two spellings of one language — blade/htmlscan emits
// the very tokens blade/scanner does — so converting a file is printing its
// tokens in the other spelling. Nothing is evaluated and nothing is guessed.
//
// A conversion is only ever handed back when it is exact, and three checks
// stand behind that word:
//
//  1. Every converted file is scanned again with the other scanner and its
//     tokens are compared with the original's (see equivalent). A construct
//     the printer got wrong, or that the other syntax cannot say, fails here.
//  2. The site is rendered before and after, with the same data, and every
//     page must come out byte for byte the same.
//  3. Blade that leans on real Laravel (@auth, a function call) is refused
//     outright on its way to the Studio syntax, which has no Laravel behind it.
//
// A site converts whole or not at all. A Blade <x-card> only ever finds
// card.blade.php, so a site left half in each syntax could have pages looking
// for components that have moved; one problem anywhere stops everything and
// nothing is returned to write.
package convert

import (
	"sort"
	"strings"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/vfs"
)

// Syntax names one of the two template syntaxes.
type Syntax string

const (
	Blade  Syntax = "blade"  // .blade.php
	Studio Syntax = "studio" // .html — the DevDojo Studio syntax
)

// ParseSyntax reads a syntax by its name, in any case: "blade" or "studio".
// "html" is the Studio syntax's older name, and the extension its files carry,
// so it stays accepted.
func ParseSyntax(name string) (Syntax, bool) {
	switch strings.ToLower(name) {
	case "blade":
		return Blade, true
	case "studio", "html":
		return Studio, true
	}
	return "", false
}

// Ext is the file extension a syntax's views carry, or "" for an unknown one.
func (s Syntax) Ext() string {
	switch s {
	case Blade:
		return ".blade.php"
	case Studio:
		return ".html"
	}
	return ""
}

// Name is what a syntax is called in a message to a person.
func (s Syntax) Name() string {
	switch s {
	case Blade:
		return "Blade"
	case Studio:
		return "the DevDojo Studio syntax"
	}
	return string(s)
}

// other returns the syntax s is converted from.
func (s Syntax) other() Syntax {
	if s == Blade {
		return Studio
	}
	return Blade
}

const (
	viewsRoot      = "resources/views/"
	pagesRoot      = viewsRoot + "pages/"
	componentsRoot = viewsRoot + "components/"
)

// Result is a conversion: the files to write and the files they replace. When
// Problems is not empty the site was not converted and both are empty.
type Result struct {
	Files    map[string]string   // new path → converted source
	Removed  []string            // the paths those files replace, sorted
	Problems []render.Diagnostic // why the site could not be converted
}

// Site converts every view under resources/views/ that is written in the other
// syntax into to. opts carries the data the before-and-after render check runs
// with — pass what the site is normally rendered with, so the check exercises
// the same branches.
//
// A site with nothing to convert returns an empty Result with no problems.
func Site(fs vfs.FS, to Syntax, opts render.Options) Result {
	res := Result{Files: map[string]string{}}
	if to.Ext() == "" {
		res.Problems = append(res.Problems, problem("", 0, 0, `unknown syntax "`+string(to)+`": convert to "blade" or "studio"`))
		return res
	}
	from := to.other()

	var sources []string
	for _, p := range fs.List(viewsRoot) {
		if strings.HasSuffix(p, from.Ext()) {
			sources = append(sources, p)
		}
	}
	if len(sources) == 0 {
		return res
	}
	sort.Strings(sources)

	target := func(p string) string { return strings.TrimSuffix(p, from.Ext()) + to.Ext() }
	for _, p := range sources {
		if _, taken := fs.Read(target(p)); taken {
			res.Problems = append(res.Problems, problem(target(p), 0, 0,
				"already exists beside "+p+"; remove one of the two before converting"))
		}
	}

	pages := discoverPages(fs)
	before := compilePages(fs, pages, nil, opts)
	if to == Studio {
		res.Problems = append(res.Problems, needsLaravel(before.Diagnostics)...)
	}

	exists := componentIndex(fs)
	for _, p := range sources {
		src, _ := fs.Read(p)
		out, problems := File(p, src, to, exists)
		res.Problems = append(res.Problems, problems...)
		res.Files[target(p)] = out
		res.Removed = append(res.Removed, p)
	}

	if len(res.Problems) == 0 {
		removed := map[string]bool{}
		for _, p := range res.Removed {
			removed[p] = true
		}
		after := compilePages(overlay{base: fs, files: res.Files, removed: removed}, pages, func(p string) string {
			if removed[p] {
				return target(p)
			}
			return p
		}, opts)
		for _, page := range pages {
			if line, same := firstDifference(before.Files[page], after.Files[page]); !same {
				res.Problems = append(res.Problems, problem(page, 0, 0,
					"would render differently after converting (output line "+itoa(line)+"); nothing was changed"))
			}
		}
	}

	if len(res.Problems) > 0 {
		res.Files, res.Removed = map[string]string{}, nil
		res.Problems = dedupe(res.Problems)
	}
	return res
}

// discoverPages lists every page of the site, in either syntax, sorted. It is
// protocol.DiscoverPages, restated here because the protocol package drives
// this one.
func discoverPages(fs vfs.FS) []string {
	var out []string
	for _, p := range fs.List(pagesRoot) {
		if strings.HasSuffix(p, Blade.Ext()) || strings.HasSuffix(p, Studio.Ext()) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// compilePages renders each page, filing its output under the page's ORIGINAL
// path so a before and an after render line up whatever the files are called
// now. rename maps an original path to where the page lives in fs (nil: it has
// not moved).
func compilePages(fs vfs.FS, pages []string, rename func(string) string, opts render.Options) render.Result {
	variants := make([]render.Variant, len(pages))
	for i, p := range pages {
		entry := p
		if rename != nil {
			entry = rename(p)
		}
		variants[i] = render.Variant{Entry: entry, Output: p}
	}
	return render.CompileVariants(fs, variants, opts)
}

// componentIndex reports which component files a site has, by path relative to
// the components folder without an extension — in either syntax, since a
// component keeps its path when it is converted.
func componentIndex(fs vfs.FS) func(base string) bool {
	set := map[string]bool{}
	for _, p := range fs.List(componentsRoot) {
		rel := strings.TrimPrefix(p, componentsRoot)
		switch {
		case strings.HasSuffix(rel, Blade.Ext()):
			set[strings.TrimSuffix(rel, Blade.Ext())] = true
		case strings.HasSuffix(rel, Studio.Ext()):
			set[strings.TrimSuffix(rel, Studio.Ext())] = true
		}
	}
	return func(base string) bool { return set[base] }
}

// needsLaravel turns the app-tier diagnostics of a Blade render into problems.
// App tier means "real Laravel finishes this" — a function call, a nested
// @props default — and an .html file has no Laravel to finish anything, so
// such a page cannot be converted.
func needsLaravel(diags []render.Diagnostic) []render.Diagnostic {
	var out []render.Diagnostic
	for _, d := range diags {
		if d.Kind != render.KindAppTier {
			continue
		}
		// A directive outside the engine's subset (render.unknownDiagnostic's
		// wording) is settled file by file, where every file is seen and not
		// only the ones a page reaches: see htmlPrinter.settle.
		if name, ok := strings.CutPrefix(d.Construct, "@"); ok &&
			strings.HasPrefix(d.Message, "directive @"+name+" is outside the DevDojo Framework subset") {
			continue
		}
		what := strings.TrimSuffix(strings.TrimSuffix(d.Message, "; requires the PHP app tier"), " requires the PHP app tier")
		out = append(out, problem(d.File, d.Line, d.Col, "needs real Laravel, which "+Studio.Name()+" does not have: "+what))
	}
	return out
}

// overlay is a site with a conversion laid over it: the converted files are
// there, the files they replace are not.
type overlay struct {
	base    vfs.FS
	files   map[string]string
	removed map[string]bool
}

func (o overlay) Read(path string) ([]byte, bool) {
	if body, ok := o.files[path]; ok {
		return []byte(body), true
	}
	if o.removed[path] {
		return nil, false
	}
	return o.base.Read(path)
}

func (o overlay) List(prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range o.base.List(prefix) {
		if !o.removed[p] && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for p := range o.files {
		if strings.HasPrefix(p, prefix) && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// firstDifference reports whether a and b are the same bytes and, when they
// are not, the 1-based line of the first byte that differs.
func firstDifference(a, b string) (line int, same bool) {
	if a == b {
		return 0, true
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return 1 + strings.Count(a[:i], "\n"), false
}

func problem(file string, line, col int, msg string) render.Diagnostic {
	return render.Diagnostic{File: file, Line: line, Col: col, Kind: render.KindError, Construct: "convert", Message: msg}
}

// dedupe drops repeated problems, keeping the first of each.
func dedupe(in []render.Diagnostic) []render.Diagnostic {
	seen := map[render.Diagnostic]bool{}
	out := in[:0]
	for _, d := range in {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// laravelDirectives is every directive real Laravel (and the packages a Laravel
// site commonly carries) compiles, lowercased, apart from the handful this
// engine renders itself. A Blade file using one of them is asking for Laravel.
// Anything not listed here Laravel prints as written — a CSS @media, an Alpine
// @click, an @handle — and so does this engine.
var laravelDirectives = func() map[string]bool {
	names := `auth endauth elseauth guest endguest elseguest can endcan elsecan cannot
		endcannot elsecannot canany endcanany elsecanany env endenv production
		endproduction unless endunless isset endisset empty endempty switch case
		default endswitch for endfor forelse endforelse while endwhile php endphp
		verbatim endverbatim include includeif includewhen includeunless
		includefirst includeisolated each extends extendsfirst section endsection
		show stop append overwrite yield parent hassection sectionmissing push
		endpush pushonce endpushonce pushif endpushif prepend endprepend
		prependonce endprependonce stack hasstack once endonce component
		endcomponent componentfirst endcomponentfirst endcomponentclass slot
		endslot aware checked selected disabled readonly required bool csrf method
		error enderror session endsession context endcontext json js dd dump lang
		endlang choice inject use fragment endfragment vitereactrefresh when
		livewire livewirestyles livewirescripts livewirescriptconfig persist
		endpersist teleport endteleport this entangle script endscript assets
		endassets island endisland volt inertia inertiahead routes feature
		endfeature role endrole hasrole endhasrole hasanyrole endhasanyrole
		hasallroles endhasallroles unlessrole endunlessrole filamentstyles
		filamentscripts`
	set := map[string]bool{}
	for _, n := range strings.Fields(names) {
		set[n] = true
	}
	return set
}()
