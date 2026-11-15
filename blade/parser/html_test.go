package parser

import (
	"reflect"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/htmlscan"
)

// parseHTML runs a Studio-syntax source through its scanner and this parser.
func parseHTML(src string) ([]ast.Node, []Diag) {
	return Parse("page.html", htmlscan.Scan("page.html", []byte(src)))
}

// TestHTMLDiagnosticsSpeakHTML: the parser is shared by both syntaxes, so a
// diagnostic must name the construct the way the FILE spells it — an .html
// author who never wrote "@if" should never read "unclosed @if".
func TestHTMLDiagnosticsSpeakHTML(t *testing.T) {
	cases := []struct {
		name, src, construct, message string
	}{
		{"unclosed if", `<If condition="a"> x`, "<If>", "unclosed <If>"},
		{"stray if closer", `x </If>`, "</If>", "stray </If> with no matching <If>"},
		{"stray else", `x <Else> y`, "<Else>", "stray <Else> with no matching <If>"},
		{"stray elseif", `x <ElseIf condition="a"> y`, "<ElseIf>", "stray <ElseIf> with no matching <If>"},
		{"unclosed foreach", `<Foreach posts="post"> x`, "<Foreach>", "unclosed <Foreach>"},
		{"stray foreach closer", `x </Foreach>`, "</Foreach>", "stray </Foreach> with no matching <Foreach>"},
		{"break outside a loop", `<Break />`, "<Break>", "<Break> outside a <Foreach>"},
		{"continue outside a loop", `<Continue />`, "<Continue>", "<Continue> outside a <Foreach>"},
		{"unclosed component", `<Card> x`, "<Card>", "unclosed <Card>"},
		{"stray component closer", `x </Card>`, "<Card>", "stray </Card> with no matching <Card>"},
		{"stray layout closer", `x </Layout>`, "<Layout>", "stray </Layout> with no matching <Layout>"},
		{"unclosed layout", `<Layout src="main.html"> x`, "<Layout>", "unclosed <Layout>"},
		{"stray slot closer", `x </Slot>`, "<Slot>", "stray </Slot> with no matching <Slot>"},
		{"unclosed slot", `<Card><Slot name="t"> x </Card>`, `<Slot name="t">`, `unclosed <Slot name="t">`},
		{"bad item name", `<Foreach posts="my post"> x </Foreach>`, "<Foreach>",
			"malformed <Foreach>: the name for each item must be letters, numbers and underscores"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := parseHTML(tc.src)
			if len(diags) != 1 {
				t.Fatalf("diags = %+v; want exactly one", diags)
			}
			d := diags[0]
			if d.Construct != tc.construct || d.Message != tc.message {
				t.Errorf("diag = %q %q; want %q %q", d.Construct, d.Message, tc.construct, tc.message)
			}
			if d.File != "page.html" || d.Line != 1 {
				t.Errorf("diag position = %s:%d:%d", d.File, d.Line, d.Col)
			}
		})
	}
}

// A <Slot name> pair outside a component tag is refused, with the HTML wording.
func TestHTMLSlotOutsideComponent(t *testing.T) {
	_, diags := parseHTML(`<Slot name="t"> x </Slot>`)
	if len(diags) == 0 {
		t.Fatal("want a diagnostic")
	}
	want := `<Slot name="t"> is only valid directly inside a component tag`
	if diags[0].Construct != `<Slot name="t">` || diags[0].Message != want {
		t.Errorf("diag = %q %q; want %q", diags[0].Construct, diags[0].Message, want)
	}
}

// TestHTMLScanErrorBecomesDiag: an Error token from the scanner surfaces as one
// diagnostic at the tag, and the content the tag wrapped is kept.
func TestHTMLScanErrorBecomesDiag(t *testing.T) {
	nodes, diags := parseHTML(`<Layout title="A">body</Layout>`)
	if len(diags) != 1 {
		t.Fatalf("diags = %+v; want exactly one", diags)
	}
	if d := diags[0]; d.Construct != "<Layout>" || d.Message != `<Layout> needs a src, like <Layout src="main.html">` || d.Line != 1 || d.Col != 1 {
		t.Errorf("diag = %+v", d)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d; want the body text alone", len(nodes))
	}
	if text, ok := nodes[0].(*ast.Text); !ok || text.Bytes != "body" {
		t.Errorf("node = %#v; want Text \"body\"", nodes[0])
	}
}

// TestHTMLForeachLimit: limit rides the loop token into the AST.
func TestHTMLForeachLimit(t *testing.T) {
	nodes, diags := parseHTML(`<Foreach posts="post" limit="3">x</Foreach>`)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v", diags)
	}
	loop, ok := nodes[0].(*ast.Foreach)
	if !ok {
		t.Fatalf("node = %#v; want a Foreach", nodes[0])
	}
	if loop.ItemsExpr != "$posts" || loop.ValVar != "$post" || loop.KeyVar != "" || loop.Limit != "3" {
		t.Errorf("loop = %+v", loop)
	}

	// A Blade loop has no limit.
	nodes, _ = parseSrc(t, `@foreach ($posts as $post) x @endforeach`)
	if loop := nodes[0].(*ast.Foreach); loop.Limit != "" {
		t.Errorf("blade loop limit = %q; want none", loop.Limit)
	}
}

// TestHTMLComponentCarriesItsFiles: a component node keeps the tag as written
// and the files it may name, for the renderer to resolve.
func TestHTMLComponentCarriesItsFiles(t *testing.T) {
	nodes, diags := parseHTML(`<SectionsHero badge="x">body</SectionsHero>`)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v", diags)
	}
	comp, ok := nodes[0].(*ast.Component)
	if !ok {
		t.Fatalf("node = %#v; want a Component", nodes[0])
	}
	if comp.Name != "sections.hero" || comp.Tag != "SectionsHero" {
		t.Errorf("component name = %q tag = %q", comp.Name, comp.Tag)
	}
	if want := []string{"sections/hero", "sections-hero"}; !reflect.DeepEqual(comp.Candidates, want) {
		t.Errorf("candidates = %v; want %v", comp.Candidates, want)
	}
	if len(comp.Body) != 1 {
		t.Errorf("body = %d nodes; want 1", len(comp.Body))
	}

	// A Blade component names exactly one file, so it carries none.
	nodes, _ = parseSrc(t, `<x-sections.hero badge="x"/>`)
	if comp := nodes[0].(*ast.Component); comp.Tag != "" || comp.Candidates != nil {
		t.Errorf("blade component tag = %q candidates = %v; want none", comp.Tag, comp.Candidates)
	}
}
