package render

import (
	"sort"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

// warningsFor runs Analyze over a single page entry against an in-memory tree
// and returns only the warning-kind diagnostics.
func warningsFor(files map[string]string) []Diagnostic {
	fs := vfs.Mem{}
	for k, v := range files {
		fs[k] = v
	}
	return Analyze(fs, []string{"resources/views/pages/index.blade.php"})
}

func TestAnalyzeUndeclaredAttributeWarns(t *testing.T) {
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":              `<x-sections.hero heading="Hi" subtitle="typo"/>`,
		"resources/views/components/sections/hero.blade.php": "@props(['heading' => 'Default'])\n<h1>{{ $heading }}</h1>",
	})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(ws), ws)
	}
	w := ws[0]
	if w.Kind != KindWarning {
		t.Errorf("kind = %q, want warning", w.Kind)
	}
	want := `undeclared attribute "subtitle" on <x-sections.hero> (not in @props)`
	if w.Message != want {
		t.Errorf("message = %q, want %q", w.Message, want)
	}
	if w.Construct != "" {
		t.Errorf("construct = %q, want empty (CLI prints message verbatim)", w.Construct)
	}
	if w.Line != 1 {
		t.Errorf("line = %d, want 1", w.Line)
	}
}

func TestAnalyzeDeclaredAttributeSilent(t *testing.T) {
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":              `<x-sections.hero heading="Hi"/>`,
		"resources/views/components/sections/hero.blade.php": "@props(['heading' => 'Default'])\n<h1>{{ $heading }}</h1>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings for a declared attribute, got %v", ws)
	}
}

func TestAnalyzeKebabAttributeMatchesCamelProp(t *testing.T) {
	// Real Blade matches foo-bar="…" to the fooBar prop; the analyzer must too.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":     `<x-card data-title="Hi"/>`,
		"resources/views/components/card.blade.php": "@props(['dataTitle' => ''])\n<div>{{ $dataTitle }}</div>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings (foo-bar → fooBar), got %v", ws)
	}
}

func TestAnalyzeBoundAttributeNotChecked(t *testing.T) {
	// Only STATIC attributes are analyzed; a bound attribute is how collection
	// data is legitimately handed in and is out of scope for the warning.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":     `<x-card :rows="$plans"/>`,
		"resources/views/components/card.blade.php": "@props(['title' => ''])\n<div>{{ $title }}</div>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings for a bound attribute, got %v", ws)
	}
}

func TestAnalyzeStaticCollidingWithBoundNotWarned(t *testing.T) {
	// A static attr whose name also appears as a bound attr on the same
	// instance is not an undeclared-attribute typo — the bound form is the
	// real pass. Don't warn on the static half.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":     `<x-card title="fallback" :title="$t"/>`,
		"resources/views/components/card.blade.php": "@props(['body' => ''])\n<div>{{ $body }}</div>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings (static collides with bound), got %v", ws)
	}
}

func TestAnalyzeNoPropsWarnsEveryStaticAttr(t *testing.T) {
	// A component with no @props declares no props: every static attribute is
	// undeclared.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":     `<x-card title="Hi"/>`,
		"resources/views/components/card.blade.php": "<div>a card</div>",
	})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %v", ws)
	}
}

func TestAnalyzeMissingComponentNoWarning(t *testing.T) {
	// A missing component is Compile's error, not a lint warning — the analyzer
	// cannot know the prop set, so it stays silent.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php": `<x-sections.nope heading="Hi"/>`,
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings for a missing component, got %v", ws)
	}
}

func TestAnalyzeUnparseablePropsNoWarning(t *testing.T) {
	// If @props cannot be parsed, the analyzer cannot know the declared set;
	// be conservative and emit no warning (Compile still reports the props
	// error on its own stream).
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":     `<x-card title="Hi"/>`,
		"resources/views/components/card.blade.php": "@props(not-a-literal)\n<div>x</div>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings for unparseable @props, got %v", ws)
	}
}

func TestAnalyzeNestedInstanceInComponentCheckedOnce(t *testing.T) {
	// A component file used by two pages contains one undeclared-attribute
	// instance internally: it must warn exactly once (the internal instance is
	// a single source location), not once per referencing page.
	fs := vfs.Mem{
		"resources/views/pages/a.blade.php":                 `<x-layouts.main/>`,
		"resources/views/pages/b.blade.php":                 `<x-layouts.main/>`,
		"resources/views/components/layouts/main.blade.php": "<x-widget bad=\"x\"/>",
		"resources/views/components/widget.blade.php":       "@props(['ok' => ''])\n<span>{{ $ok }}</span>",
	}
	ws := Analyze(fs, []string{"resources/views/pages/a.blade.php", "resources/views/pages/b.blade.php"})
	if len(ws) != 1 {
		t.Fatalf("want exactly 1 warning (checked once), got %d: %v", len(ws), ws)
	}
	if !strings.Contains(ws[0].Message, `"bad"`) {
		t.Errorf("message = %q, want it to name the bad attr", ws[0].Message)
	}
}

func TestAnalyzeSelfRecursiveComponentTerminates(t *testing.T) {
	// A component referencing itself must not loop the analyzer forever.
	fs := vfs.Mem{
		"resources/views/pages/index.blade.php":     `<x-node/>`,
		"resources/views/components/node.blade.php": "<x-node/>",
	}
	_ = Analyze(fs, []string{"resources/views/pages/index.blade.php"})
}

// A component that reads $attributes legitimately takes arbitrary extra
// attributes (they land in the bag), so undeclared-attribute warnings would
// be false positives there — the analyzer stays silent for that component.
func TestAnalyzeAttributesBagSuppressesUndeclaredWarnings(t *testing.T) {
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.php":       `<x-button type="submit" class="wide"/>`,
		"resources/views/components/button.blade.php": "@props(['type' => 'button'])\n<button {{ $attributes->merge(['type' => $type]) }}>{{ $slot }}</button>",
		"resources/views/pages/other.blade.php":       ``,
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings for a bag-consuming component, got %v", ws)
	}
}

// htmlWarnings runs Analyze over pages/index.html in an in-memory tree.
func htmlWarnings(files map[string]string) []Diagnostic {
	return Analyze(vfs.Mem(files), []string{"resources/views/pages/index.html"})
}

// TestAnalyzeHTMLUndeclaredAttributeWarns: the typo check works for the HTML
// syntax too, worded with the tags the author wrote.
func TestAnalyzeHTMLUndeclaredAttributeWarns(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":              "<SectionsHero heading=\"Hi\"\n  subtitle=\"typo\" />",
		"resources/views/components/sections/hero.html": `<Props heading="Default" /><h1>{{ heading }}</h1>`,
	})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(ws), ws)
	}
	w := ws[0]
	want := `undeclared attribute "subtitle" on <SectionsHero> (not in its <Props>)`
	if w.Kind != KindWarning || w.Message != want {
		t.Errorf("warning = %q %q; want %q", w.Kind, w.Message, want)
	}
	if w.Line != 2 || w.Col != 3 {
		t.Errorf("position = %d:%d; want 2:3 (the attribute)", w.Line, w.Col)
	}
}

// TestAnalyzeHTMLLayoutAttributes: a <Layout>'s attributes are checked against
// the layout file's <Props>, and src itself is never one of them.
func TestAnalyzeHTMLLayoutAttributes(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":             `<Layout src="main.html" title="Home" titel="typo">x</Layout>`,
		"resources/views/components/layouts/main.html": `<Props title="Untitled" /><title>{{ title }}</title>{{ slot }}`,
	})
	if len(ws) != 1 || ws[0].Message != `undeclared attribute "titel" on <Layout> (not in its <Props>)` {
		t.Fatalf("warnings = %v", ws)
	}
}

// TestAnalyzeHTMLPropsAreOptional: an HTML component with no <Props> accepts
// whatever it is given — every attribute becomes a variable — so there is
// nothing to check a passed attribute against.
func TestAnalyzeHTMLPropsAreOptional(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":     `<Card title="Hi" anything="goes" />`,
		"resources/views/components/card.html": `<h2>{{ title }}</h2>`,
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings without a <Props>, got %v", ws)
	}
}

// TestAnalyzeHTMLAttributesBagSilences: a component that prints its attributes
// bag takes extra attributes on purpose.
func TestAnalyzeHTMLAttributesBagSilences(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":       `<Button label="Go" class="big" :id="site.id" />`,
		"resources/views/components/button.html": `<Props label="x" /><a {{ attributes }}>{{ label }}</a>`,
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings when the component prints attributes, got %v", ws)
	}
}

// TestAnalyzeHTMLWalksIntoComponents: tags nested inside an HTML component, a
// loop, a condition and a slot are all reached.
func TestAnalyzeHTMLWalksIntoComponents(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":             `<Layout src="main.html"><If site.on><Foreach posts="p"><Card><Slot name="t"><Tag colour="x" /></Slot></Card></Foreach></If></Layout>`,
		"resources/views/components/layouts/main.html": `{{ slot }}<Tag color="red" colr="typo" />`,
		"resources/views/components/card.html":         `{{ t }}{{ slot }}`,
		"resources/views/components/tag.html":          `<Props color="blue" /><i>{{ color }}</i>`,
	})
	var got []string
	for _, w := range ws {
		got = append(got, w.Message)
	}
	sort.Strings(got) // the walk order is not a contract; the CLI sorts by position
	want := []string{
		`undeclared attribute "colour" on <Tag> (not in its <Props>)`,
		`undeclared attribute "colr" on <Tag> (not in its <Props>)`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestAnalyzeHTMLAmbiguousComponent: <SectionsHero> can mean sections/hero or
// sections-hero. When both files exist the folder wins, and lint says so.
func TestAnalyzeHTMLAmbiguousComponent(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":              `<SectionsHero />`,
		"resources/views/components/sections/hero.html": `folder`,
		"resources/views/components/sections-hero.html": `dash`,
	})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %v", ws)
	}
	want := `<SectionsHero> matches more than one file — using resources/views/components/sections/hero.html, not resources/views/components/sections-hero.html`
	if ws[0].Kind != KindWarning || ws[0].Message != want {
		t.Errorf("warning = %q\n want    %q", ws[0].Message, want)
	}
}

// TestAnalyzeHTMLPageCallingBladeComponent: an .html page using a Blade
// component is checked against that component's @props, by the Blade rules.
func TestAnalyzeHTMLPageCallingBladeComponent(t *testing.T) {
	ws := htmlWarnings(map[string]string{
		"resources/views/pages/index.html":           `<Badge label="new" colour="red" />`,
		"resources/views/components/badge.blade.php": "@props(['label'])\n<i>{{ $label }}</i>",
	})
	if len(ws) != 1 || ws[0].Message != `undeclared attribute "colour" on <Badge> (not in @props)` {
		t.Fatalf("warnings = %v", ws)
	}
}
