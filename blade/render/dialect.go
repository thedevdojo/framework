package render

import (
	"strings"

	"github.com/thedevdojo/framework/blade/htmlscan"
	"github.com/thedevdojo/framework/blade/scanner"
	"github.com/thedevdojo/framework/site"
)

// DevDojo Framework reads two syntaxes, chosen per file by its extension:
//
//	.blade.php   Blade   — @if, {{ $post->title }}, <x-sections.hero/>
//	.html        HTML    — <If>, {post.title}, <SectionsHero />
//
// They are two scanners in front of ONE parser, renderer and expression
// evaluator: blade/htmlscan emits the same tokens blade/scanner does for the
// equivalent Blade source. So everything below the scanner is shared, and the
// only dialect-aware code in this package is this file: picking the scanner,
// and wording a diagnostic the way the file it points at is written.

// isHTMLFile reports whether path is written in the Studio syntax.
func isHTMLFile(path string) bool {
	return strings.HasSuffix(path, ".html")
}

// scanSource tokenizes a view file with the scanner its extension selects. Any
// file that is not .html is Blade, as every file was before the HTML syntax
// existed. old, when set, names the .html files to read as they were read
// before single braces (Options.OldBraces).
func scanSource(path string, src []byte, old func(string) bool) ([]scanner.Token, htmlscan.Info) {
	if isHTMLFile(path) {
		return htmlscan.ScanWith(path, src, htmlscan.Options{Legacy: old != nil && old(path)})
	}
	return scanner.Scan(path, src), htmlscan.Info{}
}

// braceNotices is the notice for an .html file that still prints values with
// {{ }} where single braces would do: once per file, however many it holds.
func braceNotices(path string, info htmlscan.Info) []site.Notice {
	if len(info.DoubleBraces) == 0 {
		return nil
	}
	return []site.Notice{site.DoubleBracesNotice(path, len(info.DoubleBraces))}
}

// say picks the wording for a diagnostic at p: blade for a Blade file, html
// for a Studio one. An .html author never wrote "@props" or "$title", so a
// message about their file should not either.
func say(p scanner.Pos, blade, html string) string {
	if isHTMLFile(p.File) {
		return html
	}
	return blade
}

// htmlExprMessage rewords one of the evaluator's "valid PHP, but not something
// a template evaluates" messages for an .html file. The evaluator speaks Blade
// ("member ->parent requires the app tier"); an .html author wrote a dot, has
// no "$", and has no app tier to fall to — so the message drops all three and
// ends by saying what a template can do.
func htmlExprMessage(msg string) string {
	msg = strings.TrimSuffix(msg, " requires the PHP app tier")
	if trimmed := strings.TrimSuffix(msg, " requires the app tier"); trimmed != msg {
		msg = trimmed + " is not available"
	}
	msg = strings.ReplaceAll(msg, "->", ".")
	msg = strings.ReplaceAll(msg, "$", "")
	return msg + " — a template can read values and compare them (==, !=, <, >, and, or, not), nothing more"
}
