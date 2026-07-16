package render

import (
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/vfs"
)

// warningsFor runs Analyze over a single page entry against an in-memory tree
// and returns only the warning-kind diagnostics.
func warningsFor(files map[string]string) []Diagnostic {
	fs := vfs.Mem{}
	for k, v := range files {
		fs[k] = v
	}
	return Analyze(fs, []string{"resources/views/pages/index.blade.html"})
}

func TestAnalyzeUndeclaredAttributeWarns(t *testing.T) {
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.html":              `<x-sections.hero heading="Hi" subtitle="typo"/>`,
		"resources/views/components/sections/hero.blade.html": "@props(['heading' => 'Default'])\n<h1>{{ $heading }}</h1>",
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
		"resources/views/pages/index.blade.html":              `<x-sections.hero heading="Hi"/>`,
		"resources/views/components/sections/hero.blade.html": "@props(['heading' => 'Default'])\n<h1>{{ $heading }}</h1>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings for a declared attribute, got %v", ws)
	}
}

func TestAnalyzeKebabAttributeMatchesCamelProp(t *testing.T) {
	// Real Blade matches foo-bar="…" to the fooBar prop; the analyzer must too.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.html":     `<x-card data-title="Hi"/>`,
		"resources/views/components/card.blade.html": "@props(['dataTitle' => ''])\n<div>{{ $dataTitle }}</div>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings (foo-bar → fooBar), got %v", ws)
	}
}

func TestAnalyzeBoundAttributeNotChecked(t *testing.T) {
	// Only STATIC attributes are analyzed; a bound attribute is how collection
	// data is legitimately handed in and is out of scope for the warning.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.html":     `<x-card :rows="$plans"/>`,
		"resources/views/components/card.blade.html": "@props(['title' => ''])\n<div>{{ $title }}</div>",
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
		"resources/views/pages/index.blade.html":     `<x-card title="fallback" :title="$t"/>`,
		"resources/views/components/card.blade.html": "@props(['body' => ''])\n<div>{{ $body }}</div>",
	})
	if len(ws) != 0 {
		t.Fatalf("want 0 warnings (static collides with bound), got %v", ws)
	}
}

func TestAnalyzeNoPropsWarnsEveryStaticAttr(t *testing.T) {
	// A component with no @props declares no props: every static attribute is
	// undeclared.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.html":     `<x-card title="Hi"/>`,
		"resources/views/components/card.blade.html": "<div>a card</div>",
	})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %v", ws)
	}
}

func TestAnalyzeMissingComponentNoWarning(t *testing.T) {
	// A missing component is Compile's error, not a lint warning — the analyzer
	// cannot know the prop set, so it stays silent.
	ws := warningsFor(map[string]string{
		"resources/views/pages/index.blade.html": `<x-sections.nope heading="Hi"/>`,
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
		"resources/views/pages/index.blade.html":     `<x-card title="Hi"/>`,
		"resources/views/components/card.blade.html": "@props(not-a-literal)\n<div>x</div>",
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
		"resources/views/pages/a.blade.html":                 `<x-layouts.main/>`,
		"resources/views/pages/b.blade.html":                 `<x-layouts.main/>`,
		"resources/views/components/layouts/main.blade.html": "<x-widget bad=\"x\"/>",
		"resources/views/components/widget.blade.html":       "@props(['ok' => ''])\n<span>{{ $ok }}</span>",
	}
	ws := Analyze(fs, []string{"resources/views/pages/a.blade.html", "resources/views/pages/b.blade.html"})
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
		"resources/views/pages/index.blade.html":     `<x-node/>`,
		"resources/views/components/node.blade.html": "<x-node/>",
	}
	_ = Analyze(fs, []string{"resources/views/pages/index.blade.html"})
}
