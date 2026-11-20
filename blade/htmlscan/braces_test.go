package htmlscan

import (
	"fmt"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/scanner"
)

// TestSingleBracesMatchBlade: a value in single braces is the same token the
// Blade echo is, in text and in the attribute values of plain HTML.
func TestSingleBracesMatchBlade(t *testing.T) {
	cases := []struct{ name, html, blade string }{
		{"text", `<h2>{title}</h2>`, `<h2>{{ $title }}</h2>`},
		{"member", `{post.title}`, `{{ $post->title }}`},
		{"spaces inside", `{ post.title }`, `{{ $post->title }}`},
		{"default", `{tagline ?? 'Welcome'}`, `{{ $tagline ?? 'Welcome' }}`},
		{"not", `{!open}`, `{{ !$open }}`},
		{"comparison", `{a = 1}`, `{{ $a == 1 }}`},
		{"href", `<a href="{link.url}">x</a>`, `<a href="{{ $link->url }}">x</a>`},
		{"inside a class", `<a class="btn btn-{variant}">`, `<a class="btn btn-{{ $variant }}">`},
		{"inside a style", `<div style="height: {height}px">`, `<div style="height: {{ $height }}px">`},
		{"single-quoted value", `<a title='{t}'>`, `<a title='{{ $t }}'>`},
		{"unquoted value", `<a data-n={n}>`, `<a data-n={{ $n }}>`},
		{"class call", `<a {class(['btn', 'on' => active])}>`, `<a @class(['btn', 'on' => $active])>`},
		{"style call", `<a {style(['color: red' => hot])}>`, `<a @style(['color: red' => $hot])>`},
		{"two in a row", `{a}{b}`, `{{ $a }}{{ $b }}`},
		{"after a plain tag closes", `<a onclick="go()">{x}</a>`, `<a onclick="go()">{{ $x }}</a>`},
		{"raw echo is still raw", `{!! post.body !!}`, `{!! $post->body !!}`},
		{"double braces still read", `{{ x }}`, `{{ $x }}`},
		{"old escape still read", `@{{ x }}`, `@{{ x }}`},
		{"component attribute", `<Button label="Hi {name}!" />`, `<x-button :label="'Hi ' . $name . '!'" />`},
		{"component attribute alone", `<Button data-id="{id}" />`, `<x-button :data-id="'' . $id" />`},
		{"layout pass-through", `<Layout src="main.html" title="{page.title}">x</Layout>`, `<x-layouts.main :title="'' . $page->title">x</x-layouts.main>`},
		{"the attributes of a script tag", `<script src="{base}/a.js"></script>`, `<script src="{{ $base }}/a.js"></script>`},
		{"after a script ends", `<script>{a}</script>{b}`, `<script>{a}</script>{{ $b }}`},
		{"after a comment ends", `<!-- {a} -->{b}`, `<!-- {a} -->{{ $b }}`},
		{"after a code attribute", `<a x-text="{a}" title="{b}">`, `<a x-text="{a}" title="{{ $b }}">`},
		{"title element", `<title>{page.title}</title>`, `<title>{{ $page->title }}</title>`},
		{"component bound attribute is unchanged", `<Button :items="plans" />`, `<x-button :items="$plans" />`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shape(Scan("page.html", []byte(tc.html)))
			want := shape(scanner.Scan("page.blade.php", []byte(tc.blade)))
			if got != want {
				t.Errorf("html  %s\nblade %s\n--- html ---\n%s--- blade ---\n%s", tc.html, tc.blade, got, want)
			}
		})
	}
}

// TestSingleBracesLeftAlone: wherever a brace is not the template's, or what
// is inside is not an expression, the bytes come back as one Text token.
func TestSingleBracesLeftAlone(t *testing.T) {
	cases := []string{
		`{}`,
		`{ }`,
		`{ open: false }`,
		`{"a": 1, "b": 2}`,
		`a {b c} d`,
		`{post.title`,
		`{'}'}`,
		`.card { color: red; }`,
		`<style>.a { color: {c}; }</style>`,
		`<script>if (a) { x() } const y = {z};</script>`,
		`<SCRIPT>{z}</SCRIPT>`,
		`<Script>{z}</Script>`,
		`<!-- {hidden} -->`,
		`<div x-data="{ open: false }" x-text="{label}" :class="{on}" @click="{go}" v-bind="{v}" onclick="f({a})">x</div>`,
		`<button ONCLICK="{a}" X-INIT="{b}">x</button>`,
		`<a onclick={a}>x</a>`,
		`@click="open = !open"`,
		`a\b`,
		`<script>/\{x}/</script>`,
		`<a onclick="/\{/">x</a>`,
	}
	for _, src := range cases {
		toks := Scan("page.html", []byte(src))
		if len(toks) != 1 || toks[0].Kind != scanner.Text || toks[0].Text != src {
			t.Errorf("Scan(%q) =\n%s want it back as one Text token", src, shape(toks))
		}
	}
}

// TestSingleBraceEscape: \{ is a literal brace where a brace would be read,
// and two plain bytes everywhere else.
func TestSingleBraceEscape(t *testing.T) {
	cases := []struct{ src, text string }{
		{`\{title}`, `{title}`},
		{`<a title="\{x}">`, `<a title="{x}">`},
		{`\{\{ x }}`, `{{ x }}`},
		{`\\{x}`, `\{x}`},
		{`a\{`, `a{`},
		{`a\b`, `a\b`},
		{`<script>\{x}</script>`, `<script>\{x}</script>`},
		{`<a x-on:click="\{x}">`, `<a x-on:click="\{x}">`},
	}
	for _, tc := range cases {
		var out strings.Builder
		for _, tok := range Scan("page.html", []byte(tc.src)) {
			if tok.Kind != scanner.Text {
				t.Fatalf("Scan(%q) gave a %v token", tc.src, tok.Kind)
			}
			out.WriteString(tok.Text)
		}
		if out.String() != tc.text {
			t.Errorf("Scan(%q) prints %q, want %q", tc.src, out.String(), tc.text)
		}
	}

	// On a component the escape works in an attribute value too.
	got := shape(Scan("page.html", []byte(`<Button label="\{x} {y}" />`)))
	want := shape(scanner.Scan("page.blade.php", []byte(`<x-button :label="'{x} ' . $y" />`)))
	if got != want {
		t.Errorf("component escape:\n got  %s\n want %s", got, want)
	}
	got = shape(Scan("page.html", []byte(`<Button label="\{x}" />`)))
	want = shape(scanner.Scan("page.blade.php", []byte(`<x-button label="{x}" />`)))
	if got != want {
		t.Errorf("component escape alone:\n got  %s\n want %s", got, want)
	}
}

// TestCodeAttributesOnComponents: on a component tag an attribute that holds
// code keeps its braces, as it does on plain HTML.
func TestCodeAttributesOnComponents(t *testing.T) {
	got := shape(Scan("page.html", []byte(`<Button x-text="{a}" @click="{b}" ::class="{c}" onclick="{d}" />`)))
	want := shape(scanner.Scan("page.blade.php", []byte(`<x-button x-text="{a}" @click="{b}" ::class="{c}" onclick="{d}" />`)))
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestScanReportsDoubleBraces: {{ x }} where {x} would do is reported, by
// position, so the engine can say once per file that migrate rewrites it.
// Where a single brace is not read, {{ }} is the only way, and not reported.
func TestScanReportsDoubleBraces(t *testing.T) {
	_, info := ScanWith("page.html", []byte("{x}\n{{ a }} <b title=\"{{ c }}\"></b><Button label=\"{{ d }}\" /><script>{{ e }}</script><i onclick=\"{{ f }}\"></i>{!! g !!}"), Options{})
	var got []string
	for _, p := range info.DoubleBraces {
		got = append(got, fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col))
	}
	if want := "page.html:2:1 page.html:2:19 page.html:2:40"; strings.Join(got, " ") != want {
		t.Errorf("double braces at %v, want %s", got, want)
	}
}

// TestLegacyScanReadsNoSingleBraces: how an .html file was read before single
// braces, which migrate needs to know what a site printed.
func TestLegacyScanReadsNoSingleBraces(t *testing.T) {
	for _, src := range []string{`{title} \{x}`, `<a href="{u}">`} {
		toks, _ := ScanWith("page.html", []byte(src), Options{Legacy: true})
		if len(toks) != 1 || toks[0].Kind != scanner.Text || toks[0].Text != src {
			t.Errorf("legacy Scan(%q) = %s", src, shape(toks))
		}
	}
	toks, _ := ScanWith("page.html", []byte(`<Button label="{x}" />`), Options{Legacy: true})
	if a := toks[0].Attrs[0]; a.Bound || a.Value != "{x}" {
		t.Errorf("legacy component attribute = %+v", a)
	}
}
