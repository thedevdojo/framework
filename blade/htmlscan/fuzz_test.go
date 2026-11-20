package htmlscan

import (
	"regexp"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/scanner"
)

// templateTag matches the start of anything Scan may treat as a tag of the
// template: a capitalized tag name, or the two lowercase DevDojo Static
// spellings.
var templateTag = regexp.MustCompile(`</?[A-Z]|(?i)</?(layout|include)`)

// FuzzScan holds the HTML scanner to its three promises on any input: it never
// panics, its tokens never run backwards, and a file with nothing of the
// template's in it comes back as the exact bytes that went in. Read the way
// it was before single braces (Options.Legacy), only {{, {!! and a template
// tag are the template's; read today, any brace may be.
func FuzzScan(f *testing.F) {
	seeds := []string{
		`{{ post.title }}`,
		`<If condition="a">x<ElseIf condition="b">y<Else>z</If>`,
		`<If featured name="Tony" :plan="site.plan" not="draft">x</If>`,
		`<Foreach posts="post" limit="3">{{ post.title }}</Foreach>`,
		`<Foreach collection="posts" as="post" key="i"><Break /><Continue condition="a" /></Foreach>`,
		`<Layout src="main.html" title="Home"><Slot name="head"><meta></Slot>body</Layout>`,
		`<include src="button.html"></include>`,
		`<Props heading="Hello" items :count="3" /><Vite src="a.css, b.js" />`,
		`<SectionsHero badge="Hi {{ name }}" :items="posts" />`,
		`<h2>{title}</h2><a href="{link.url}" class="btn-{variant}" x-data="{ open: false }">\\{x}</a>`,
		`<Button label="Hi {name} \\{x}" x-text="{a}" />{!open}{ open: false }{}`,
		`<Sections.Hero><Slot /></Sections.Hero>`,
		`<Button>Text Here</Button>`,
		`<script>if (a<B && c>d) { x = "<If>" }</script>`,
		`<style>@media (min-width: 1px) { a > b {} }</style>`,
		`<!-- <If condition="a"> {{ x }} -->`,
		`@{{ raw }} {{-- gone --}} {!! html !!}`,
		`<If`, `<If condition="`, `</`, `<`, `<Foreach posts=>`, `<Layout src="../x">`,
		`<Slot name="a b">`, `</If></Foreach></Card></Slot></Layout>`,
		`<A.B.C.D.E.F.G.H />`, `<ABCDEFGHIJKLMNOP />`, `<If ::class="x">`,
		"line1\n<Card title=\"a > b\"\n  class='x'/>\nline4",
		``,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks := Scan("page.html", []byte(src)) // must not panic
		legacy, _ := ScanWith("page.html", []byte(src), Options{Legacy: true})

		last := scanner.Pos{Line: 1, Col: 1}
		for _, tk := range toks {
			if tk.Pos.Line < last.Line || (tk.Pos.Line == last.Line && tk.Pos.Col < last.Col) {
				t.Fatalf("positions run backwards: %+v after %+v in %q", tk.Pos, last, src)
			}
			last = tk.Pos
			if tk.Pos.File != "page.html" {
				t.Fatalf("token %v has file %q", tk.Kind, tk.Pos.File)
			}
		}

		// Plain HTML is not the template's to touch.
		if strings.Contains(src, "{{") || strings.Contains(src, "{!!") || templateTag.MatchString(src) {
			return
		}
		plain := func(toks []scanner.Token, how string) {
			var out strings.Builder
			for _, tk := range toks {
				if tk.Kind != scanner.Text {
					t.Fatalf("plain HTML produced a %v token (%q) from %q, %s", tk.Kind, tk.Name, src, how)
				}
				out.WriteString(tk.Text)
			}
			if out.String() != src {
				t.Fatalf("plain HTML changed, %s:\n in  %q\n out %q", how, src, out.String())
			}
		}
		plain(legacy, "read the old way")
		if !strings.Contains(src, "{") {
			plain(toks, "read today")
		}
	})
}

// FuzzLiteral: any text written through Literal scans back as exactly that
// text. (The converter hands Literal each run of text whole, so the fuzz does
// too: a context is set up by what the text itself starts with.)
func FuzzLiteral(f *testing.F) {
	for _, seed := range []string{
		`{title} \\{x} {{ x }} @{{ y }} {!! z !!} {{-- c --}} { open: false }`,
		`<a title="{x}" x-text="{y}">{z}<script>{a}</script>{b}<!-- {c} -->{d}`,
		`<script>{x}</script>{y}`,
		`<Button>{!x}</Button><If a>\\`,
		`<a {class(['a'])} onclick={x} title={y}>`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		var ctx Context
		got := Literal(text, &ctx)
		var printed strings.Builder
		for _, tok := range Scan("x.html", []byte(got)) {
			switch tok.Kind {
			case scanner.Text:
				printed.WriteString(tok.Text)
			case scanner.RawEcho:
				s := strings.TrimSuffix(strings.TrimPrefix(tok.Text, "'"), "'")
				printed.WriteString(strings.TrimPrefix(s, `\`))
			default:
				t.Fatalf("Literal(%q) = %q scans to a %v token", text, got, tok.Kind)
			}
		}
		if printed.String() != text {
			t.Fatalf("Literal(%q) = %q prints %q", text, got, printed.String())
		}
	})
}
