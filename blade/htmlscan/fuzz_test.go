package htmlscan

import (
	"regexp"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// templateTag matches the start of anything Scan may treat as a tag of the
// template: a capitalized tag name, or the two lowercase DevDojo Static
// spellings.
var templateTag = regexp.MustCompile(`</?[A-Z]|(?i)</?(layout|include)`)

// FuzzScan holds the HTML scanner to its three promises on any input: it never
// panics, its tokens never run backwards, and a file with nothing of the
// template's in it comes back as the exact bytes that went in.
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
		var out strings.Builder
		for _, tk := range toks {
			if tk.Kind != scanner.Text {
				t.Fatalf("plain HTML produced a %v token (%q) from %q", tk.Kind, tk.Name, src)
			}
			out.WriteString(tk.Text)
		}
		if out.String() != src {
			t.Fatalf("plain HTML changed:\n in  %q\n out %q", src, out.String())
		}
	})
}

// FuzzExpr: rewriting a friendly expression never panics, and an expression
// already written the Blade way is left exactly as it was.
func FuzzExpr(f *testing.F) {
	for _, seed := range []string{
		`post.title`, `name = 'Tony'`, `a and not b or c`, `$post->title`, `count > 2`,
		`attributes.merge(['class' => 'btn'])`, `"a.b" . c`, `1.5 + x.y`, `a === b`, `a !== b`,
		`'`, `"`, `a.`, `.a`, `((`, `x ?? 'y'`, ``,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		once := Expr(src) // must not panic
		if twice := Expr(once); twice != once {
			t.Fatalf("Expr is not stable:\n src   %q\n once  %q\n twice %q", src, once, twice)
		}
	})
}
