package content

import (
	"errors"
	"testing"
)

// TestParseFrontmatterScalars pins the scalar-value grammar: quoted strings are
// unquoted, bare strings stay strings, integers become int64, floats float64,
// and true/false become bool. Duplicate keys are last-wins.
func TestParseFrontmatterScalars(t *testing.T) {
	src := []byte("---\n" +
		"title: 'Hello World'\n" +
		"author: \"Ada\"\n" +
		"slug: bare-string\n" +
		"count: 42\n" +
		"rank: 4.5\n" +
		"draft: false\n" +
		"pinned: true\n" +
		"title: Winner\n" + // duplicate → last wins
		"---\n" +
		"# Body\n")
	fm, err := ParseFrontmatter(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{
		"title":  "Winner",
		"author": "Ada",
		"slug":   "bare-string",
		"count":  int64(42),
		"rank":   4.5,
		"draft":  false,
		"pinned": true,
	}
	if len(fm.Fields) != len(want) {
		t.Fatalf("fields = %#v; want %#v", fm.Fields, want)
	}
	for k, v := range want {
		if fm.Fields[k] != v {
			t.Errorf("field %q = %#v (%T); want %#v (%T)", k, fm.Fields[k], fm.Fields[k], v, v)
		}
	}
	if fm.Body != "# Body\n" {
		t.Errorf("body = %q; want %q", fm.Body, "# Body\n")
	}
}

// TestParseFrontmatterMissing: a file with no leading `---` fence has no
// frontmatter — Fields is empty and the whole input is the body.
func TestParseFrontmatterMissing(t *testing.T) {
	src := []byte("# Just a heading\n\nA paragraph.\n")
	fm, err := ParseFrontmatter(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Fields) != 0 {
		t.Errorf("fields = %#v; want empty", fm.Fields)
	}
	if fm.Body != string(src) {
		t.Errorf("body = %q; want whole file %q", fm.Body, src)
	}
}

// TestParseFrontmatterNested: a non-scalar value (an indented child block, or a
// flow array/map) is out of the v1 subset and yields ErrNested.
func TestParseFrontmatterNested(t *testing.T) {
	cases := map[string]string{
		"indented child": "---\nauthor:\n  name: Bob\n---\nbody\n",
		"flow array":     "---\ntags: [a, b]\n---\nbody\n",
		"flow map":       "---\nmeta: {x: 1}\n---\nbody\n",
		"bare key":       "---\nauthor:\n---\nbody\n",
	}
	for name, src := range cases {
		if _, err := ParseFrontmatter([]byte(src)); !errors.Is(err, ErrNested) {
			t.Errorf("%s: err = %v; want ErrNested", name, err)
		}
	}
}

// TestRenderMarkdown pins goldmark output for the agreed subset so a dependency
// bump that changes rendering is caught here.
func TestRenderMarkdown(t *testing.T) {
	cases := map[string]string{
		"# Hi":                  "<h1>Hi</h1>\n",
		"Hello **bold** world.": "<p>Hello <strong>bold</strong> world.</p>\n",
		"[link](/x)":            "<p><a href=\"/x\">link</a></p>\n",
	}
	for md, want := range cases {
		if got := RenderMarkdown(md); got != want {
			t.Errorf("RenderMarkdown(%q) = %q; want %q", md, got, want)
		}
	}
}

// TestRenderMarkdownRawHTML: raw HTML must pass through (WithUnsafe), so a site
// can drop a raw block into markdown content.
func TestRenderMarkdownRawHTML(t *testing.T) {
	got := RenderMarkdown("<div class=\"note\">hi</div>")
	if got != "<div class=\"note\">hi</div>" {
		t.Errorf("raw HTML passthrough = %q", got)
	}
}

// TestRenderCollection: items are ordered by filename ascending, links are
// derived from the dir + basename, content is rendered, and a nested-frontmatter
// file is skipped and reported.
func TestRenderCollection(t *testing.T) {
	files := []File{
		{Name: "b-second.md", Data: []byte("---\ntitle: Second\n---\n## Two\n")},
		{Name: "a-first.md", Data: []byte("---\ntitle: First\n---\n## One\n")},
		{Name: "c-bad.md", Data: []byte("---\nauthor:\n  name: Bob\n---\nnope\n")},
	}
	items, errs := RenderCollection("post", files)
	if len(items) != 2 {
		t.Fatalf("items = %d; want 2", len(items))
	}
	// Ordered by filename ascending: a-first, b-second.
	if items[0].Fields["title"] != "First" || items[1].Fields["title"] != "Second" {
		t.Errorf("order wrong: %q, %q", items[0].Fields["title"], items[1].Fields["title"])
	}
	if items[0].Link != "/post/a-first" || items[1].Link != "/post/b-second" {
		t.Errorf("links = %q, %q", items[0].Link, items[1].Link)
	}
	if items[0].Content != "<h2>One</h2>\n" {
		t.Errorf("content = %q", items[0].Content)
	}
	if len(errs) != 1 || errs[0].Name != "c-bad.md" || !errors.Is(errs[0].Err, ErrNested) {
		t.Errorf("errs = %#v; want one ErrNested for c-bad.md", errs)
	}
}
