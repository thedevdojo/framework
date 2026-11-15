package htmlscan

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// shape renders the parts of a token stream both syntaxes must agree on —
// everything the parser and renderer act on — leaving out positions and the
// HTML-only bookkeeping (Tag, Candidates, Src).
func shape(toks []scanner.Token) string {
	var b strings.Builder
	for _, tk := range toks {
		fmt.Fprintf(&b, "%d name=%q text=%q self=%v", int(tk.Kind), tk.Name, tk.Text, tk.SelfClose)
		for _, a := range tk.Attrs {
			fmt.Fprintf(&b, " [%s=%q bound=%v valueless=%v]", a.Name, a.Value, a.Bound, a.Valueless)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestScanMatchesBlade is the heart of the Studio syntax: every tag is another
// spelling of a Blade construct, so scanning the HTML form must yield exactly
// the tokens the Blade scanner yields for the Blade form.
func TestScanMatchesBlade(t *testing.T) {
	cases := []struct{ name, html, blade string }{
		// output
		{"echo", `Hello {{ site.name }}!`, `Hello {{ $site->name }}!`},
		{"raw echo", `{!! post.content !!}`, `{!! $post->content !!}`},
		{"comment", `a{{-- note --}}b`, `a{{-- note --}}b`},
		{"escaped braces", `@{{ raw }}`, `@{{ raw }}`},
		{"echo with default", `{{ tagline ?? 'Welcome' }}`, `{{ $tagline ?? 'Welcome' }}`},

		// conditions
		{"if else",
			"<If condition=\"a\">\nA\n<Else>\nB\n</If>",
			"@if($a)\nA\n@else\nB\n@endif"},
		{"if elseif else",
			"<If condition=\"user.plan == 'pro'\">\nA\n<ElseIf condition=\"user.plan == 'team'\">\nB\n<Else>\nC\n</If>",
			"@if($user->plan == 'pro')\nA\n@elseif($user->plan == 'team')\nB\n@else\nC\n@endif"},
		{"single equals", `<If condition="name='Tony'">`, `@if($name=='Tony')`},
		{"self-closing dividers", "<If condition=\"a\">\nA\n<Else />\nB\n</If>", "@if($a)\nA\n@else\nB\n@endif"},
		{"truthy shorthand", `<If featured>`, `@if($featured)`},
		{"truthy shorthand, dotted", `<If post.featured>`, `@if($post->featured)`},
		{"equals shorthand", `<If name="Tony">`, `@if($name == 'Tony')`},
		{"equals shorthand, dotted", `<If post.status="published">`, `@if($post->status == 'published')`},
		{"equals shorthand, quote in value", `<If name="O'Brien">`, `@if($name == 'O\'Brien')`},
		{"equals shorthand, number", `<If count="3">`, `@if($count == 3)`},
		{"equals shorthand, boolean", `<If active="false">`, `@if($active == false)`},
		{"equals shorthand, bound", `<If :plan="site.plan">`, `@if($plan == ($site->plan))`},
		{"not", `<If not="post.draft">`, `@if(!($post->draft))`},
		{"several attributes are ANDed", `<If featured status="published">`, `@if(($featured) && ($status == 'published'))`},

		// loops
		{"foreach", `<Foreach posts="post"> x </Foreach>`, `@foreach($posts as $post) x @endforeach`},
		{"foreach, key and value", `<Foreach site="key, value">`, `@foreach($site as $key => $value)`},
		{"foreach, arrow key and value", `<Foreach site="key => value">`, `@foreach($site as $key => $value)`},
		{"foreach, dotted collection", `<Foreach post.tags="tag">`, `@foreach($post->tags as $tag)`},
		{"foreach, Static long form", `<ForEach collection="faq" as="item">`, `@foreach($faq as $item)`},
		{"foreach, Static default item name", `<ForEach collection="menu">`, `@foreach($menu as $menu)`},
		{"foreach, long form with key", `<Foreach collection="posts[0].tags" as="tag" key="i">`, `@foreach($posts[0]->tags as $i => $tag)`},
		{"break", "<Break />\n", "@break\n"},
		{"continue with condition", `<Continue condition="loop.first" />`, `@continue($loop->first)`},
		{"break with shorthand", `<Break loop.last />`, `@break($loop->last)`},

		// components
		{"component", `<Button>Go</Button>`, `<x-button>Go</x-button>`},
		{"component in a folder",
			`<SectionsHero badge="Personal Blog" :items="plans" wide />`,
			`<x-sections.hero badge="Personal Blog" :items="$plans" wide />`},
		{"component, dotted tag", `<Sections.Hero />`, `<x-sections.hero />`},
		{"escaped colon attribute", `<Button ::class="{ on: open }" />`, `<x-button ::class="{ on: open }" />`},

		// layouts and includes
		{"layout", `<Layout src="main.html" title="About">x</Layout>`, `<x-layouts.main title="About">x</x-layouts.main>`},
		{"layout, lowercase like Static", `<layout title="About" src="main.html">x</layout>`, `<x-layouts.main title="About">x</x-layouts.main>`},
		{"layout in a subfolder", `<Layout src="marketing/wide.html">x</Layout>`, `<x-layouts.marketing.wide>x</x-layouts.marketing.wide>`},
		{"include", `<Include src="sections/hero.html" badge="Hi" />`, `<x-sections.hero badge="Hi" />`},
		{"include, lowercase like Static", `<include src="button.html"></include>`, `<x-button></x-button>`},
		{"nested layouts close in order",
			`<Layout src="a"><Layout src="b">x</Layout></Layout>`,
			`<x-layouts.a><x-layouts.b>x</x-layouts.b></x-layouts.a>`},

		// slots
		{"named slot", `<Card><Slot name="title">T</Slot>body</Card>`, `<x-card><x-slot:title>T</x-slot>body</x-card>`},
		{"print the default slot", `<main><Slot /></main>`, `<main>{{ $slot }}</main>`},
		{"print a named slot", `<Slot name="sidebar" />`, `{{ $sidebar ?? '' }}`},
		{"print a dashed named slot", `<Slot name="side-bar" />`, `{{ $sideBar ?? '' }}`},

		// props and assets
		{"props",
			`<Props heading="Hello" cta="Learn more" items :count="3" logo-text="Hi" :tags="['a', 'b']" />`,
			`@props(['heading' => 'Hello', 'cta' => 'Learn more', 'items' => null, 'count' => 3, 'logoText' => 'Hi', 'tags' => ['a', 'b']])`},
		{"props, quote in default", `<Props label="it's" />`, `@props(['label' => 'it\'s'])`},
		{"vite", `<Vite src="resources/css/site.css" />`, `@vite(['resources/css/site.css'])`},
		{"vite, several entries", `<Vite src="a.css, b.css" />`, `@vite(['a.css', 'b.css'])`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shape(Scan("page.html", []byte(tc.html)))
			want := shape(scanner.Scan("page.blade.php", []byte(tc.blade)))
			if got != want {
				t.Errorf("html  %s\nblade %s\n--- html tokens ---\n%s--- blade tokens ---\n%s", tc.html, tc.blade, got, want)
			}
		})
	}
}

// TestScanLeavesHTMLAlone: only capitalized tags (plus the two lowercase Static
// spellings) and the brace forms are the template's. Everything else — plain
// HTML, Alpine's @click, CSS at-rules, emails, even a Blade <x-tag> — is text.
func TestScanLeavesHTMLAlone(t *testing.T) {
	cases := []string{
		`<div class="a"><slot></slot><button type="button">x</button></div>`,
		`<a href="mailto:hello@example.com" @click="open = true" x-on:keydown.escape="close()">hi</a>`,
		`@if($x) @foreach @media (min-width: 640px) { } @@ @endif`,
		`<x-button label="x" />`,
		`<svg><linearGradient id="g"></linearGradient><clipPath></clipPath></svg>`,
		`<!DOCTYPE html><html lang="en"></html>`,
		`<!-- <If condition="x">{{ y }}</If> <Button /> -->`,
		`1 < 2 and 3 > 2`,
		`<layout-grid></layout-grid><includes></includes>`,
		`a <Button b ! c`,
		`</ Button>`,
		`<`,
		`</`,
	}
	for _, src := range cases {
		toks := Scan("page.html", []byte(src))
		if len(toks) != 1 || toks[0].Kind != scanner.Text || toks[0].Text != src {
			t.Errorf("Scan(%q) = %s; want it back as one Text token", src, shape(toks))
		}
	}
}

// TestScanScriptAndStyle: inside <script> and <style> a capitalized tag is
// code, not a component (a<B>c, Promise<Response>), but the brace forms and the
// control tags still work — a JSON-LD block can loop.
func TestScanScriptAndStyle(t *testing.T) {
	src := `<script>if (a<B>c) { new Map<Key>(); } var n = "{{ site.name }}";<Foreach faqs="faq">{{ faq.q }}</Foreach></script><Button />`
	want := []struct {
		kind scanner.Kind
		text string
	}{
		{scanner.Text, `<script>if (a<B>c) { new Map<Key>(); } var n = "`},
		{scanner.Echo, `$site->name`},
		{scanner.Text, `";`},
		{scanner.Directive, `$faqs as $faq`},
		{scanner.Echo, `$faq->q`},
		{scanner.Directive, ``},
		{scanner.Text, `</script>`},
		{scanner.TagOpen, ``},
	}
	toks := Scan("page.html", []byte(src))
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d:\n%s", len(toks), len(want), shape(toks))
	}
	for i, w := range want {
		if toks[i].Kind != w.kind || toks[i].Text != w.text {
			t.Errorf("token %d = kind %d text %q; want kind %d text %q", i, toks[i].Kind, toks[i].Text, w.kind, w.text)
		}
	}

	// <style> behaves the same, and an uppercase <STYLE> is still a style tag.
	for _, src := range []string{`<style>a<B>c{}</style>`, `<STYLE>a<B>c{}</STYLE>`} {
		toks := Scan("page.html", []byte(src))
		if len(toks) != 1 || toks[0].Kind != scanner.Text || toks[0].Text != src {
			t.Errorf("Scan(%q) = %s; want one Text token", src, shape(toks))
		}
	}
}

// TestScanRecovers: a half-written tag never swallows the rest of the file.
func TestScanRecovers(t *testing.T) {
	toks := Scan("page.html", []byte(`a <Button b {{ x }} <If condition="y"> z </If>`))
	got := shape(toks)
	want := shape(scanner.Scan("page.blade.php", []byte(`a <Button b {{ $x }} @if($y) z @endif`)))
	if got != want {
		t.Errorf("got\n%swant\n%s", got, want)
	}
}

// TestScanTagBookkeeping: HTML tokens remember the tag as written (so a
// diagnostic can say <If>, not @if) and components carry their file candidates.
func TestScanTagBookkeeping(t *testing.T) {
	toks := Scan("page.html", []byte(`<If condition="a"><ElseIf condition="b"><Else></If><Foreach posts="post"></Foreach><SectionsHero></SectionsHero><Layout src="main.html"></Layout><Card><Slot name="t"></Slot></Card><Break />`))
	var labels []string
	for _, tk := range toks {
		labels = append(labels, tk.Label())
	}
	want := []string{
		"<If>", "<ElseIf>", "<Else>", "</If>",
		"<Foreach>", "</Foreach>",
		"<SectionsHero>", "</SectionsHero>",
		"<Layout>", "</Layout>",
		"<Card>", `<Slot name="t">`, "</Slot>", "</Card>",
		"<Break>",
	}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("labels = %q\n want  %q", labels, want)
	}

	hero := Scan("page.html", []byte(`<SectionsHero />`))[0]
	if !reflect.DeepEqual(hero.Candidates, []string{"sections/hero", "sections-hero"}) {
		t.Errorf("component candidates = %v", hero.Candidates)
	}
	layout := Scan("page.html", []byte(`<Layout src="main.html" />`))[0]
	if !reflect.DeepEqual(layout.Candidates, []string{"layouts/main"}) {
		t.Errorf("layout candidates = %v", layout.Candidates)
	}
	include := Scan("page.html", []byte(`<Include src="sections/hero.html" />`))[0]
	if !reflect.DeepEqual(include.Candidates, []string{"sections/hero"}) {
		t.Errorf("include candidates = %v", include.Candidates)
	}
}

// TestScanAttributeInterpolation: {{ }} inside a text attribute is sugar for a
// bound attribute that joins the pieces — title="Hi {{ name }}" IS
// :title="'Hi ' . name" — so it scans to the very token Blade's bound concat
// scans to. Each piece is text, so a lone {{ n }} is still joined onto ”.
func TestScanAttributeInterpolation(t *testing.T) {
	cases := []struct{ html, blade string }{
		{`<Button href="/posts/{{ post.slug }}?a={!! raw !!}" />`, `<x-button :href="'/posts/' . $post->slug . '?a=' . $raw" />`},
		{`<Button label="{{ n }} items" />`, `<x-button :label="$n . ' items'" />`},
		{`<Button data-id="{{ id }}" />`, `<x-button :data-id="'' . $id" />`},
		{`<Button label="{{ a ?? 'b' }}!" />`, `<x-button :label="($a ?? 'b') . '!'" />`},
		{`<Button label="{{ a == 1 }}{{ posts[0].title }}" />`, `<x-button :label="($a == 1) . $posts[0]->title" />`},
		{`<Button label="it's {{ n }} o\clock" />`, `<x-button :label="'it\'s ' . $n . ' o\\clock'" />`},
		{`<Button label="Plain" :n="count" open />`, `<x-button label="Plain" :n="$count" open />`},
		{`<Button label="half {{ open" />`, `<x-button label="half {{ open" />`},
	}
	for _, c := range cases {
		got := shape(Scan("page.html", []byte(c.html)))
		want := shape(scanner.Scan("page.blade.php", []byte(c.blade)))
		if got != want {
			t.Errorf("%s\n  got  %s\n  want %s", c.html, got, want)
		}
	}
}

// TestScanForeachLimit: limit (and Static's count) ride along on the loop
// token for the parser to lift into the AST.
func TestScanForeachLimit(t *testing.T) {
	for _, src := range []string{`<Foreach posts="post" limit="3">`, `<ForEach collection="posts" as="post" count="3">`} {
		tok := Scan("page.html", []byte(src))[0]
		if tok.Kind != scanner.Directive || tok.Name != "foreach" || tok.Text != "$posts as $post" {
			t.Errorf("%s → %s", src, shape([]scanner.Token{tok}))
		}
		if len(tok.Attrs) != 1 || tok.Attrs[0].Name != "limit" || tok.Attrs[0].Value != "3" {
			t.Errorf("%s limit attrs = %+v", src, tok.Attrs)
		}
	}
	tok := Scan("page.html", []byte(`<Foreach posts="post" limit="site.per_page">`))[0]
	if len(tok.Attrs) != 1 || tok.Attrs[0].Value != "$site->per_page" {
		t.Errorf("limit expression = %+v", tok.Attrs)
	}
}

// TestScanErrors: a tag missing what it needs becomes one Error token saying
// what to write, and its closing tag is swallowed so the mistake is reported
// once and the content inside still renders.
func TestScanErrors(t *testing.T) {
	cases := []struct {
		src       string
		construct string
		message   string
		rest      string // the Blade form of what should remain
	}{
		{`<If> x </If>`, "<If>", `<If> needs a condition, like <If condition="post.featured">`, `@if(false) x @endif`},
		{`<If condition=""> x </If>`, "<If>", `<If> needs a condition, like <If condition="post.featured">`, `@if(false) x @endif`},
		{`<Layout title="A">x</Layout>`, "<Layout>", `<Layout> needs a src, like <Layout src="main.html">`, `x`},
		{`<Layout src="../x.html">x</Layout>`, "<Layout>", `<Layout> src "../x.html" is not a file inside the layouts folder`, `x`},
		{`<Layout :src="name">x</Layout>`, "<Layout>", `<Layout> src must be a plain file name, like <Layout src="main.html">`, `x`},
		{`<Include />`, "<Include>", `<Include> needs a src, like <Include src="button.html">`, ``},
		{`<Foreach>x</Foreach>`, "<Foreach>", `<Foreach> needs a list and a name for each item, like <Foreach posts="post">`, `x`},
		{`<Foreach posts>x</Foreach>`, "<Foreach>", `<Foreach> needs a list and a name for each item, like <Foreach posts="post">`, `x`},
		{`<Foreach posts="post" pages="page">x</Foreach>`, "<Foreach>", `<Foreach> loops over one list at a time`, `x`},
		{`<Foreach collection="posts[0].tags">x</Foreach>`, "<Foreach>", `<Foreach> needs a name for each item, like as="item"`, `x`},
		{`<Slot>x</Slot>`, "<Slot>", `<Slot> needs a name to fill a slot, like <Slot name="title">; <Slot /> prints the default slot`, `x`},
	}
	for _, tc := range cases {
		toks := Scan("page.html", []byte(tc.src))
		if len(toks) == 0 || toks[0].Kind != scanner.Error {
			t.Errorf("%s: first token = %s; want an Error", tc.src, shape(toks))
			continue
		}
		if toks[0].Name != tc.construct || toks[0].Text != tc.message {
			t.Errorf("%s: error = %q %q; want %q %q", tc.src, toks[0].Name, toks[0].Text, tc.construct, tc.message)
		}
		got := shape(toks[1:])
		want := shape(scanner.Scan("page.blade.php", []byte(tc.rest)))
		if got != want {
			t.Errorf("%s: remaining tokens\n%swant\n%s", tc.src, got, want)
		}
	}
}

// TestScanPositions: every token points at the first byte of its construct,
// 1-based, exactly like the Blade scanner.
func TestScanPositions(t *testing.T) {
	src := "<Layout src=\"main.html\">\n  <If featured>\n    {{ title }}\n  </If>\n</Layout>"
	want := []struct {
		kind      scanner.Kind
		line, col int
	}{
		{scanner.TagOpen, 1, 1},
		{scanner.Text, 1, 25},
		{scanner.Directive, 2, 3},
		{scanner.Text, 2, 16},
		{scanner.Echo, 3, 5},
		{scanner.Text, 3, 16},
		{scanner.Directive, 4, 3},
		{scanner.Text, 4, 8},
		{scanner.TagClose, 5, 1},
	}
	toks := Scan("page.html", []byte(src))
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d:\n%s", len(toks), len(want), shape(toks))
	}
	for i, w := range want {
		if toks[i].Kind != w.kind || toks[i].Pos.Line != w.line || toks[i].Pos.Col != w.col || toks[i].Pos.File != "page.html" {
			t.Errorf("token %d = kind %d at %s:%d:%d; want kind %d at %d:%d", i, toks[i].Kind, toks[i].Pos.File, toks[i].Pos.Line, toks[i].Pos.Col, w.kind, w.line, w.col)
		}
	}

	// An attribute points at its own name.
	tag := Scan("page.html", []byte("<Button\n  label=\"x\" />"))[0]
	if p := tag.Attrs[0].Pos; p.Line != 2 || p.Col != 3 {
		t.Errorf("attr pos = %d:%d; want 2:3", p.Line, p.Col)
	}
}

// TestScanShoutedHTML: HTML written in capitals — old markup, a pasted email
// template — is still HTML. A real HTML element in ALL CAPS is left alone;
// only a capitalized name that is not one (or is mixed-case, the way a
// component is written) belongs to the template.
func TestScanShoutedHTML(t *testing.T) {
	plain := []string{
		`<BR>`, `<HR />`, `<P>`, `<DIV CLASS="x">a</DIV>`, `<H1>t</H1>`, `<A HREF="#">x</A>`,
		`<TABLE><TR><TD>x</TD></TR></TABLE>`, `<IMG SRC="a.png" />`, `<CENTER><FONT>x</FONT></CENTER>`,
		`<HTML><HEAD><TITLE>t</TITLE></HEAD><BODY></BODY></HTML>`,
	}
	for _, src := range plain {
		toks := Scan("page.html", []byte(src))
		if len(toks) != 1 || toks[0].Kind != scanner.Text || toks[0].Text != src {
			t.Errorf("Scan(%q) = %s; want it back as one Text token", src, shape(toks))
		}
	}

	components := map[string]string{
		`<FAQ />`: "faq", `<CTA />`: "cta", `<Button />`: "button", `<Div />`: "div",
		`<Nav />`: "nav", `<Table />`: "table",
	}
	for src, name := range components {
		toks := Scan("page.html", []byte(src))
		if len(toks) != 1 || toks[0].Kind != scanner.TagOpen || toks[0].Name != name {
			t.Errorf("Scan(%q) = %s; want component %q", src, shape(toks), name)
		}
	}

	// The built-in tags are matched in any capitalization, shouted included.
	for _, src := range []string{`<IF condition="a">x</IF>`, `<SLOT />`, `<FOREACH posts="post">x</FOREACH>`} {
		toks := Scan("page.html", []byte(src))
		if len(toks) == 0 || toks[0].Kind == scanner.Text {
			t.Errorf("Scan(%q) = %s; want a built-in tag", src, shape(toks))
		}
	}
}

// TestScanPropsEntities: a text default in <Props> is read the way HTML reads
// an attribute — &quot; is a double quote, &#39; and &apos; an apostrophe,
// &amp; an ampersand — so a default can hold both kinds of quotes. Nothing
// else is decoded, and a :bound default is an expression, left as written.
func TestScanPropsEntities(t *testing.T) {
	cases := []struct{ html, blade string }{
		{`<Props body="can't say &quot;better&quot;" />`, `@props(['body' => 'can\'t say "better"'])`},
		{`<Props body='it&#39;s &apos;fine&apos; &amp; done' />`, `@props(['body' => 'it\'s \'fine\' & done'])`},
		{`<Props body="Q&A &lt; &copy; &amp;quot;" />`, `@props(['body' => 'Q&A &lt; &copy; &quot;'])`},
		{`<Props :items="['&quot;']" />`, `@props(['items' => ['&quot;']])`},
	}
	for _, c := range cases {
		got := shape(Scan("c.html", []byte(c.html)))
		want := shape(scanner.Scan("c.blade.php", []byte(c.blade)))
		if got != want {
			t.Errorf("%s\n  got  %s\n  want %s", c.html, got, want)
		}
	}
}
