package convert

import (
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/vfs"
)

// have builds the "does this component exist" lookup File takes from a list of
// component paths (relative to the components folder, no extension).
func have(bases ...string) func(string) bool {
	set := map[string]bool{}
	for _, b := range bases {
		set[b] = true
	}
	return func(base string) bool { return set[base] }
}

type pair struct {
	name, in, want string
	have           []string
}

func runPairs(t *testing.T, to Syntax, path string, pairs []pair) {
	t.Helper()
	for _, c := range pairs {
		t.Run(c.name, func(t *testing.T) {
			got, problems := File(path, []byte(c.in), to, have(c.have...))
			if len(problems) > 0 {
				t.Fatalf("problems: %+v\noutput: %s", problems, got)
			}
			if got != c.want {
				t.Errorf("\n in:   %q\n got:  %q\n want: %q", c.in, got, c.want)
			}
		})
	}
}

// TestBladeToHTML: each Blade construct comes out as the Studio syntax's own
// spelling of it, in the shortest form that means exactly the same thing.
func TestBladeToHTML(t *testing.T) {
	runPairs(t, Studio, "src/pages/index.blade.php", []pair{
		{name: "echo", in: `{{ $post->title }}`, want: `{{ post.title }}`},
		{name: "echo unspaced", in: `{{$post->title}}`, want: `{{ post.title }}`},
		{name: "raw echo", in: `{!! $post->body !!}`, want: `{!! post.body !!}`},
		{name: "comment", in: `a{{-- a {{ $note }} --}}b`, want: `a{{-- a {{ $note }} --}}b`},
		{name: "default", in: `{{ $tagline ?? 'Welcome' }}`, want: `{{ tagline ?? 'Welcome' }}`},

		{name: "if equals text",
			in:   "@if ($plan == 'pro')\nA\n@elseif ($n > 2)\nB\n@else\nC\n@endif\n",
			want: "<If plan=\"pro\">\nA\n<ElseIf condition=\"n > 2\">\nB\n<Else>\nC\n</If>\n"},
		{name: "if truthy path", in: `@if ($post->featured) x @endif`, want: `<If post.featured> x </If>`},
		{name: "if truthy name", in: `@if($featured) x @endif`, want: `<If featured> x </If>`},
		{name: "if equals number", in: `@if ($count == 3) x @endif`, want: `<If count="3"> x </If>`},
		{name: "if equals numeric text", in: `@if ($count == '3') x @endif`, want: `<If condition="count == '3'"> x </If>`},
		{name: "if and", in: `@if ($a && !$b) x @endif`, want: `<If condition="a and !b"> x </If>`},
		{name: "if with double quotes", in: `@if ($a == "x") y @endif`, want: `<If condition='a == "x"'> y </If>`},
		{name: "if on a reserved name", in: `@if ($not) y @endif`, want: `<If condition="$not"> y </If>`},

		{name: "foreach", in: "@foreach ($posts as $post)\n{{ $post->title }}\n@endforeach\n",
			want: "<Foreach posts=\"post\">\n{{ post.title }}\n</Foreach>\n"},
		{name: "foreach key", in: `@foreach ($site->plans as $slug => $plan) x @endforeach`,
			want: `<Foreach site.plans="slug, plan"> x </Foreach>`},
		{name: "foreach expression", in: `@foreach ($posts[0]->tags as $tag) x @endforeach`,
			want: `<Foreach collection="posts[0].tags" as="tag"> x </Foreach>`},
		{name: "foreach expression key", in: `@foreach ($a ?? [] as $k => $v) x @endforeach`,
			want: `<Foreach collection="a ?? []" as="v" key="k"> x </Foreach>`},
		{name: "foreach over a reserved name", in: `@foreach ($limit as $x) y @endforeach`,
			want: `<Foreach collection="limit" as="x"> y </Foreach>`},
		{name: "break and continue",
			in:   `@foreach ($a as $b)@continue($loop->first)@break($loop->index >= 3) @break @endforeach`,
			want: `<Foreach a="b"><Continue loop.first /><Break condition="loop.index >= 3" /> <Break /> </Foreach>`},

		// The escapes only Blade needs are taken back out, so a page that came
		// from the Studio syntax returns to it as it was written.
		{name: "hatched at-sign", in: "<style>\n{!! '@' !!}media (min-width: 1px) { a { b: c } }\n</style>",
			want: "<style>\n@media (min-width: 1px) { a { b: c } }\n</style>"},
		{name: "hatched tag", in: `{!! '<' !!}x-card> and {!! '<' !!}Button> and {!! '<' !!}?xml ?>`,
			want: `<x-card> and {!! '<' !!}Button> and <?xml ?>`},
		{name: "hatched braces", in: `{!! '{' !!}@{{ three }}} and {!! '@' !!}@{{ x }}`,
			want: `@{{{ three }}} and @@{{ x }}`},
		{name: "hatch that is still needed", in: `a{!! '@' !!}{{ $x }} b{!! '{' !!}{{ $x }}`,
			want: `a{!! '@' !!}{{ x }} b{!! '{' !!}{{ x }}`},
		{name: "another one-character echo is left alone", in: "{!! 'x' !!}{!! '\n' !!}{{ 'x' }}",
			want: "{!! 'x' !!}{!! '\n' !!}{{ 'x' }}"},
		{name: "separators", in: `@if ($a)yes{{ '' }}@endif,@if ($b)no{{ '' }}@else{{ '' }}else{{ '' }}@endif.`,
			want: `<If a>yes</If>,<If b>no<Else>else</If>.`},
		{name: "separator after a line break", in: "@if ($b)\nno\n@else\n{{ '' }}(x)\n@endif\n",
			want: "<If b>\nno\n<Else>\n(x)\n</If>\n"},
		// A tag swallows the line break after it; an echo does not. Taking
		// this one out would lose a line from the page.
		{name: "separator holding a line break stays", in: "@if ($a){{ '' }}\nx\n@endif{{ '' }}{{-- c --}}\ny",
			want: "<If a>{{ '' }}\nx\n</If>{{ '' }}{{-- c --}}\ny"},
		{name: "separator between text and a line break", in: "x{{ '' }}\ny",
			want: "x\ny"},

		// @foreach with @break($loop->index >= N) first is <Foreach limit>.
		{name: "limit", in: "@foreach ($items as $x)\n@break($loop->index >= 2)\n{{ $x }}\n@endforeach\n",
			want: "<Foreach items=\"x\" limit=\"2\">\n{{ x }}\n</Foreach>\n"},
		{name: "limit inline", in: `@foreach ($items as $k => $x)@break($loop->index >= $site->max) {{ $x }} @endforeach`,
			want: `<Foreach items="k, x" limit="site.max"> {{ x }} </Foreach>`},
		{name: "limit expression", in: `@foreach ($a ?? [] as $x)@break($loop->index >= ($n + 1)) y @endforeach`,
			want: `<Foreach collection="a ?? []" as="x" limit="n + 1"> y </Foreach>`},
		{name: "limit not taken when the loop asks its length",
			in:   `@foreach ($items as $x)@break($loop->index >= 2) {{ $loop->last }} @endforeach`,
			want: `<Foreach items="x"><Break condition="loop.index >= 2" /> {{ loop.last }} </Foreach>`},
		{name: "limit not taken when it reads the item",
			in:   `@foreach ($items as $x)@break($loop->index >= $x) y @endforeach`,
			want: `<Foreach items="x"><Break condition="loop.index >= x" /> y </Foreach>`},
		{name: "limit not taken from a later break",
			in:   `@foreach ($items as $x) a @break($loop->index >= 2) @endforeach`,
			want: `<Foreach items="x"> a <Break condition="loop.index >= 2" /> </Foreach>`},
		{name: "limit not taken from another condition",
			in:   `@foreach ($items as $x)@break($loop->index > 2) y @endforeach`,
			want: `<Foreach items="x"><Break condition="loop.index > 2" /> y </Foreach>`},

		// Joined text in a bound attribute is what an .html author writes with
		// {{ }} in the value: the two are one token, so the shorter is used.
		{name: "joined attribute", in: `<x-card :title="'Hi ' . $name . '!'" :href="'/posts/' . $post->slug" />`,
			want: `<Card title="Hi {{ name }}!" href="/posts/{{ post.slug }}" />`, have: []string{"card"}},
		{name: "joined attribute, value alone", in: `<x-card :data-id="'' . $id" />`,
			want: `<Card data-id="{{ id }}" />`, have: []string{"card"}},
		{name: "joined attribute, an expression", in: `<x-card :title="'By ' . ($author ?? 'us') . '.'" />`,
			want: `<Card title="By {{ author ?? 'us' }}." />`, have: []string{"card"}},
		{name: "joined attribute, double-quoted text stays bound", in: `<x-card :title='"Say \"hi\" " . $name' />`,
			want: `<Card :title='"Say \"hi\" " . name' />`, have: []string{"card"}},
		{name: "joined attribute, apostrophe", in: `<x-card :title='"x" . $name' :note="'it\'s ' . $name" />`,
			want: `<Card :title='"x" . name' note="it's {{ name }}" />`, have: []string{"card"}},
		{name: "joined attribute stays bound without text", in: `<x-card :title="$a . $b" :n="$n" :t="'plain'" />`,
			want: `<Card :title="a . b" :n="n" :t="'plain'" />`, have: []string{"card"}},
		{name: "joined attribute stays bound when unspaced", in: `<x-card :title="'Hi '.$name" />`,
			want: `<Card :title="'Hi '.name" />`, have: []string{"card"}},
		{name: "joined attribute stays bound when the text holds braces", in: `<x-card :title="'a {{ b ' . $name" />`,
			want: `<Card :title="'a {{ b ' . name" />`, have: []string{"card"}},

		{name: "props",
			in:   `@props(['title' => 'Untitled', 'count' => 3, 'badge' => null, 'items' => [], 'open' => false, "kind" => "a", 'req'])`,
			want: `<Props title="Untitled" :count="3" badge :items="[]" :open="false" kind="a" req />`},
		{name: "props multi-line",
			in:   "@props([\n    'eyebrow' => 'FAQ',\n    'say' => 'He said \"hi\"',\n    'items',\n])\n<p></p>",
			want: "<Props\n    eyebrow=\"FAQ\"\n    say='He said \"hi\"'\n    items\n/>\n<p></p>"},
		{name: "props text with both kinds of quotes",
			in:   `@props(['body' => "If you can't say what \"better\" means", 'q' => 'Q&A', 'e' => 'a &amp; b', 'k' => 'say "&quot;" & it\'s'])`,
			want: `<Props body="If you can't say what &quot;better&quot; means" q="Q&A" e="a &amp;amp; b" k="say &quot;&amp;quot;&quot; & it's" />`},
		{name: "vite array", in: `@vite(['src/css/theme.css', 'src/css/site.css'])`,
			want: `<Vite src="src/css/theme.css, src/css/site.css" />`},
		{name: "vite string", in: `@vite('src/css/site.css')`, want: `<Vite src="src/css/site.css" />`},
		{name: "class", in: `<div @class(['a', 'b' => $on])>`, want: `<div {{ class(['a', 'b' => on]) }}>`},
		{name: "style", in: `<div @style(['color: red' => $hot])>`, want: `<div {{ style(['color: red' => hot]) }}>`},

		{name: "layout",
			in:   "<x-layouts.main title=\"Home\">\nHi\n</x-layouts.main>\n",
			want: "<Layout src=\"main.html\" title=\"Home\">\nHi\n</Layout>\n"},
		{name: "nested layout", in: `<x-layouts.marketing.wide>x</x-layouts.marketing.wide>`,
			want: `<Layout src="marketing/wide.html">x</Layout>`},
		{name: "component", in: `<x-button href="/x" :items="$posts" wide />`,
			want: `<Button href="/x" :items="posts" wide />`, have: []string{"button"}},
		{name: "component in a folder", in: `<x-sections.hero heading="Hi">x</x-sections.hero>`,
			want: `<SectionsHero heading="Hi">x</SectionsHero>`, have: []string{"sections/hero"}},
		{name: "component with a dash", in: `<x-pricing-table />`,
			want: `<PricingTable />`, have: []string{"pricing-table"}},
		{name: "folder and dash both exist", in: `<x-pricing-table />`,
			want: `<Include src="pricing-table.html" />`, have: []string{"pricing-table", "pricing/table"}},
		{name: "dot keeps a folder apart", in: `<x-pricing-table.row />`,
			want: `<PricingTable.Row />`, have: []string{"pricing-table/row", "pricing/table-row"}},
		{name: "component named like a built-in", in: `<x-if />`, want: `<Include src="if.html" />`, have: []string{"if"}},
		{name: "component named like an element", in: `<x-a href="/">x</x-a>`,
			want: `<Include src="a.html" href="/">x</Include>`, have: []string{"a"}},
		{name: "component keeps its line breaks",
			in:   "<x-card\n    title=\"A\"\n    :n=\"$count\"\n/>",
			want: "<Card\n    title=\"A\"\n    :n=\"count\"\n/>", have: []string{"card"}},
		{name: "alpine attributes ride along", in: `<x-card @click.prevent="go" ::class="{ on: open }" x-data='{ "a": 1 }' />`,
			want: `<Card @click.prevent="go" ::class="{ on: open }" x-data='{ "a": 1 }' />`, have: []string{"card"}},
		{name: "slots", in: `<x-card><x-slot:head>H</x-slot>B</x-card>`,
			want: `<Card><Slot name="head">H</Slot>B</Card>`, have: []string{"card"}},
		{name: "default slot", in: `<div>{{ $slot }}</div>`, want: `<div><Slot /></div>`},

		{name: "escaped directive", in: `<button @@click="open = !open">`, want: `<button @click="open = !open">`},
		{name: "css at-rule", in: "<style>\n@media (min-width: 640px) { a { b: c } }\n</style>", want: "<style>\n@media (min-width: 640px) { a { b: c } }\n</style>"},
		{name: "escaped braces", in: `@{{ raw }}`, want: `@{{ raw }}`},
		{name: "text that looks like a tag", in: `<p>Use <Button> here</p>`, want: `<p>Use {!! '<' !!}Button> here</p>`},
		{name: "html comment with a value in it", in: `<!-- {{ $x }} -->`, want: `{!! '<' !!}!-- {{ x }} -->`},
		{name: "plain html comment", in: `<!-- note -->`, want: `<!-- note -->`},
		{name: "component inside a script", in: `<script><x-analytics /></script>`,
			want: `<script><Include src="analytics.html" /></script>`, have: []string{"analytics"}},
		{name: "brace before a value", in: `@@{{ $x }}`, want: `@{!! '@' !!}{{ x }}`},
	})
}

// TestHTMLToBlade: each tag comes out as the Blade it is another spelling of,
// and text Blade would take for its own is defused so it prints unchanged.
func TestHTMLToBlade(t *testing.T) {
	runPairs(t, Blade, "src/pages/index.html", []pair{
		{name: "echo", in: `{{ post.title }}`, want: `{{ $post->title }}`},
		{name: "raw echo", in: `{!! post.body !!}`, want: `{!! $post->body !!}`},
		{name: "comment", in: `a{{-- note --}}b`, want: `a{{-- note --}}b`},

		// An empty class list is a bare @class: real Laravel prints class=""
		// for that and stops with an error on @class().
		{name: "empty class", in: `<a {{ class() }}>x</a> {{ style() }}(y) {{ class() }}z`,
			want: `<a @class>x</a> @style{{ '' }}(y) @class{{ '' }}z`},
		// A character the Studio syntax had to print through a raw echo goes
		// back to being text where Blade reads it as text anyway.
		{name: "hatch Blade does not need", in: "{!! '<' !!}!-- a {{ class(['b']) }} -->{!! '<' !!}If>",
			want: "<!-- a @class(['b']) --><If>"},
		{name: "hatch Blade needs too", in: `{!! '<' !!}x-card> {!! '@' !!}if {!! '{' !!}{ a`,
			want: `{!! '<' !!}x-card> @@if {!! '{' !!}{ a`},

		{name: "if else",
			in:   "<If name=\"Tony\">\nA\n<ElseIf condition=\"n > 2\">\nB\n<Else>\nC\n</If>\n",
			want: "@if ($name == 'Tony')\nA\n@elseif ($n > 2)\nB\n@else\nC\n@endif\n"},
		{name: "if shorthand", in: `<If post.featured> x </If>`, want: `@if ($post->featured) x @endif`},
		{name: "if several", in: `<If featured post.status="live"> x </If>`,
			want: `@if (($featured) && ($post->status == 'live')) x @endif`},
		{name: "if words", in: `<If condition="a = 1 and not b"> x </If>`, want: `@if ($a == 1 && ! $b) x @endif`},

		{name: "foreach", in: "<Foreach posts=\"post\">\n{{ post.title }}\n</Foreach>\n",
			want: "@foreach ($posts as $post)\n{{ $post->title }}\n@endforeach\n"},
		{name: "foreach key", in: `<Foreach plans="slug, plan"> x </Foreach>`, want: `@foreach ($plans as $slug => $plan) x @endforeach`},
		{name: "foreach long form", in: `<Foreach collection="posts" as="post" key="i"> x </Foreach>`,
			want: `@foreach ($posts as $i => $post) x @endforeach`},
		{name: "foreach limit",
			in:   "<Foreach posts=\"post\" limit=\"3\">\n{{ post.title }}\n</Foreach>\n",
			want: "@foreach ($posts as $post)\n@break($loop->index >= 3)\n{{ $post->title }}\n@endforeach\n"},
		{name: "foreach limit inline", in: `<Foreach posts="post" limit="site.max"> x </Foreach>`,
			want: `@foreach ($posts as $post)@break($loop->index >= $site->max) x @endforeach`},
		{name: "break and continue", in: `<Foreach a="b"><Continue loop.first /><Break condition="b.stop" /><Break /></Foreach>`,
			want: `@foreach ($a as $b)@continue($loop->first)@break($b->stop)@break{{ '' }}@endforeach`},

		{name: "props", in: `<Props title="Untitled" :count="3" badge />`,
			want: `@props(['title' => 'Untitled', 'count' => 3, 'badge' => null])`},
		{name: "props text with an apostrophe", in: `<Props body="There's more" say='A "b"' />`,
			want: `@props(['body' => "There's more", 'say' => 'A "b"'])`},
		{name: "props text with both kinds of quotes", in: `<Props body="If you can't say what &quot;better&quot; means &amp; more" />`,
			want: `@props(['body' => 'If you can\'t say what "better" means & more'])`},
		{name: "props text php would expand", in: `<Props price="It's $5" path="C:\it's" />`,
			want: `@props(['price' => 'It\'s $5', 'path' => 'C:\\it\'s'])`},
		{name: "props multi-line", in: "<Props\n    title=\"Untitled\"\n    badge\n/>\n<p></p>",
			want: "@props([\n    'title' => 'Untitled',\n    'badge' => null,\n])\n<p></p>"},
		{name: "vite", in: `<Vite src="src/css/theme.css, src/css/site.css" />`,
			want: `@vite(['src/css/theme.css', 'src/css/site.css'])`},
		{name: "class", in: `<div {{ class(['a', 'b' => on]) }}>`, want: `<div @class(['a', 'b' => $on])>`},
		{name: "class hard against a word", in: `<div{{ class(['a']) }}>`, want: `<div{{ '' }}@class(['a'])>`},

		{name: "layout", in: "<Layout src=\"main.html\" title=\"Home\">\nHi\n</Layout>\n",
			want: "<x-layouts.main title=\"Home\">\nHi\n</x-layouts.main>\n"},
		{name: "lowercase layout and include", in: `<layout src="main"><include src="button.html"></include></layout>`,
			want: `<x-layouts.main><x-button></x-button></x-layouts.main>`},
		{name: "component", in: `<SectionsHero heading="Hi" :items="posts" wide />`,
			want: `<x-sections.hero heading="Hi" :items="$posts" wide />`, have: []string{"sections/hero"}},
		{name: "component by dash", in: `<PricingTable>x</PricingTable>`,
			want: `<x-pricing-table>x</x-pricing-table>`, have: []string{"pricing-table"}},
		{name: "component not found yet", in: `<PricingTable />`, want: `<x-pricing.table />`},
		{name: "component keeps its line breaks",
			in:   "<Card\n    title=\"A\"\n    :n=\"count\"\n/>",
			want: "<x-card\n    title=\"A\"\n    :n=\"$count\"\n/>"},
		{name: "text with a value in it", in: `<Card title="Hi {{ name }}" data-id="{{ id }}" />`,
			want: `<x-card :title="'Hi ' . $name" :data-id="'' . $id" />`},
		{name: "alpine attributes ride along", in: `<Card @click.prevent="go" ::class="{ on: open }" />`,
			want: `<x-card @click.prevent="go" ::class="{ on: open }" />`},
		{name: "slots", in: `<Card><Slot name="head">H</Slot>B</Card>`, want: `<x-card><x-slot:head>H</x-slot>B</x-card>`},
		{name: "print slots", in: `<div><Slot /><Slot name="head" /></div>`, want: `<div>{{ $slot }}{{ $head ?? '' }}</div>`},

		{name: "alpine on plain html", in: `<button @click="open = !open" x-on:keydown.escape="close">`,
			want: `<button @@click="open = !open" x-on:keydown.escape="close">`},
		{name: "email", in: `<a href="mailto:hi@example.com">hi@example.com</a>`, want: `<a href="mailto:hi@example.com">hi@example.com</a>`},
		{name: "css at-rule", in: "@media (min-width: 640px) {", want: "{!! '@' !!}media (min-width: 640px) {"},
		{name: "css at-rule glued", in: "@media(min-width: 640px) {", want: "@@media(min-width: 640px) {"},
		{name: "doubled at", in: `@@if`, want: `{!! '@' !!}@@if`},
		{name: "literal braces", in: `@{{ raw }}`, want: `@{{ raw }}`},
		{name: "literal at and braces", in: `@@{{ raw }}`, want: `{!! '@' !!}@{{ raw }}`},
		{name: "blade tag as text", in: `<p>{{ '' }}</p><x-card> and </x-card>`,
			want: `<p>{{ '' }}</p>{!! '<' !!}x-card> and {!! '<' !!}/x-card>`},
		{name: "php open tag", in: `<?xml version="1.0"?>`, want: `{!! '<' !!}?xml version="1.0"?>`},
		{name: "word after else", in: `<If a>x<Else>more</If>`, want: `@if ($a)x{{ '' }}@else{{ '' }}more{{ '' }}@endif`},
		{name: "paren after else", in: "<If a> x <Else>\n (note)</If>", want: "@if ($a) x @else\n {{ '' }}(note)@endif"},
		{name: "tags back to back", in: `<If a><If b>!</If></If>`, want: `@if ($a)@if ($b)!@endif{{ '' }}@endif`},
		{name: "escaped braces then a value", in: `@{{ '' }}{{ x }}`, want: `@{{ '' }}{{ $x }}`},
		{name: "at before a tag", in: `@<If a> x </If>`, want: `{!! '@' !!}@if ($a) x @endif`},
	})
}

// TestProblems: what cannot be said exactly in the other syntax is reported,
// with the place, rather than converted into something slightly different.
func TestProblems(t *testing.T) {
	cases := []struct {
		name, path, in string
		to             Syntax
		wantMsg        string
		line, col      int
	}{
		{name: "laravel directive", path: "p.blade.php", to: Studio, in: "a\n  @auth\nb\n@endauth",
			wantMsg: "@auth needs real Laravel", line: 2, col: 3},
		{name: "both quote kinds", path: "p.blade.php", to: Studio, in: `@if ($a == "x" || $b == 'y') z @endif`,
			wantMsg: "both kinds of quotes", line: 1, col: 1},
		// "as => $x" has an arrow and no key. Blade rejects the loop; dropping
		// the arrow would quietly turn it into a loop that runs.
		{name: "foreach with an arrow and no key", path: "p.blade.php", to: Studio, in: "a\n@foreach ($items as => $x) y @endforeach",
			wantMsg: `@foreach is not in the form "$list as $item"`, line: 2, col: 1},
		{name: "tag missing its src", path: "p.html", to: Blade, in: "x\n<Layout>y</Layout>",
			wantMsg: "<Layout> needs a src", line: 2, col: 1},
		{name: "limit with loop.last", path: "p.html", to: Blade,
			in:      `<Foreach posts="post" limit="3"><If loop.last>x</If></Foreach>`,
			wantMsg: "loop.last", line: 1, col: 1},
		{name: "limit that reads the item", path: "p.html", to: Blade,
			in:      `<Foreach posts="post" limit="post.n">x</Foreach>`,
			wantMsg: "limit", line: 1, col: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, problems := File(c.path, []byte(c.in), c.to, have())
			if len(problems) == 0 {
				t.Fatalf("want a problem")
			}
			p := problems[0]
			if !strings.Contains(p.Message, c.wantMsg) || p.Line != c.line || p.Col != c.col || p.File != c.path || p.Kind != render.KindError {
				t.Errorf("problem = %+v; want %q at %d:%d", p, c.wantMsg, c.line, c.col)
			}
		})
	}

	// A limit is fine when the loop inside it has its own loop.last.
	_, problems := File("p.html", []byte(`<Foreach a="b" limit="2"><Foreach b="c"><If loop.last>x</If></Foreach></Foreach>`), Blade, have())
	if len(problems) != 0 {
		t.Errorf("inner loop.last: %+v", problems)
	}
}

// site is a small Blade site that touches every construct.
func bladeSite() vfs.Mem {
	return vfs.Mem{
		"src/data/site.json": `{"name": "Acme", "posts": [{"title": "A & B", "featured": true}, {"title": "C", "featured": false}, {"title": "D", "featured": true}]}`,
		"src/css/site.css":   "body { margin: 0 }",
		"src/pages/index.blade.php": `<x-layouts.main title="Home">
    <x-slot:head><meta name="x" content="{{ $site->name }}"></x-slot>
    @foreach ($site->posts as $i => $post)
        @continue($loop->first)
        <x-post-card :post="$post" :title="'#' . $i . ' ' . $post->title" class="card" />
    @endforeach
    <x-button href="/all">All {{ $site->name }} posts</x-button>
    <button @@click="go">Go</button>
    @@media and hi@example.com
</x-layouts.main>
`,
		"src/pages/about.blade.php": "<x-layouts.main>\n@if ($site->name == 'Acme')\nAbout {{ $site->name }}\n@else\nElsewhere\n@endif\n</x-layouts.main>\n",
		"src/components/layouts/main.blade.php": `@props(['title' => 'Site'])
<html>
<head>
<title>{{ $title }}</title>
{{ $head ?? '' }}
@vite(['src/css/site.css'])
</head>
<body>{{ $slot }}</body>
</html>
`,
		"src/components/post-card.blade.php": `@props([
    'post' => null,
    'title' => '',
])
<article {{ $attributes }}>
    <h2 @class(['big' => $post->featured, 'plain'])>{{ $title }}</h2>
</article>
`,
		"src/components/button.blade.php": "@props(['href' => '#'])\n<a href=\"{{ $href }}\" {{ $attributes->merge(['class' => 'btn']) }}>{{ $slot }}</a>\n",
	}
}

// TestSiteRoundTrip: a whole site goes to the Studio syntax and back. Each
// direction renders the same bytes as the site it started from (Site itself
// refuses to answer otherwise), and coming back lands on the original files.
func TestSiteRoundTrip(t *testing.T) {
	src := bladeSite()
	pages := []string{"src/pages/about.blade.php", "src/pages/index.blade.php"}
	want := render.Compile(src, pages, render.Options{})
	if len(want.Diagnostics) != 0 {
		t.Fatalf("the test site should be clean: %+v", want.Diagnostics)
	}

	toHTML := Site(src, Studio, render.Options{})
	if len(toHTML.Problems) != 0 {
		t.Fatalf("to html: %+v", toHTML.Problems)
	}
	if len(toHTML.Files) != 5 || len(toHTML.Removed) != 5 {
		t.Fatalf("to html: %d files, %d removed", len(toHTML.Files), len(toHTML.Removed))
	}
	htmlSite := apply(src, toHTML)
	for p := range htmlSite {
		if strings.HasSuffix(p, ".blade.php") {
			t.Errorf("%s was left behind", p)
		}
	}
	index := htmlSite["src/pages/index.html"]
	for _, wantLine := range []string{
		`<Layout src="main.html" title="Home">`,
		`<Slot name="head">`,
		`<Foreach site.posts="i, post">`,
		`<Continue loop.first />`,
		`<PostCard :post="post" title="#{{ i }} {{ post.title }}" class="card" />`,
		`<Button href="/all">All {{ site.name }} posts</Button>`,
		`<button @click="go">Go</button>`,
		`@media and hi@example.com`,
		`</Layout>`,
	} {
		if !strings.Contains(index, wantLine) {
			t.Errorf("index.html is missing %q:\n%s", wantLine, index)
		}
	}
	got := render.Compile(htmlSite, []string{"src/pages/about.html", "src/pages/index.html"}, render.Options{})
	for name, body := range want.Files {
		if got.Files[name] != body {
			t.Errorf("%s renders differently as Studio syntax:\n%s\n--- want ---\n%s", name, got.Files[name], body)
		}
	}

	back := Site(htmlSite, Blade, render.Options{})
	if len(back.Problems) != 0 {
		t.Fatalf("back to blade: %+v", back.Problems)
	}
	bladeAgain := apply(htmlSite, back)
	for p, body := range src {
		if bladeAgain[p] != body {
			t.Errorf("%s did not come back the same:\n%s\n--- want ---\n%s", p, bladeAgain[p], body)
		}
	}
}

// apply returns site with a conversion carried out.
func apply(site vfs.Mem, res Result) vfs.Mem {
	out := vfs.Mem{}
	for p, body := range site {
		out[p] = body
	}
	for _, p := range res.Removed {
		delete(out, p)
	}
	for p, body := range res.Files {
		out[p] = body
	}
	return out
}

// TestSiteIsAllOrNothing: one file that cannot be converted stops the whole
// site — a half-converted site would have Blade pages looking for components
// that are now .html.
func TestSiteIsAllOrNothing(t *testing.T) {
	src := bladeSite()
	src["src/components/gate.blade.php"] = "@auth\nsecret\n@endauth\n"
	res := Site(src, Studio, render.Options{})
	if len(res.Problems) == 0 {
		t.Fatal("want a problem")
	}
	if len(res.Files) != 0 || len(res.Removed) != 0 {
		t.Errorf("nothing should be converted: %d files, %d removed", len(res.Files), len(res.Removed))
	}
	if p := res.Problems[0]; p.File != "src/components/gate.blade.php" || p.Line != 1 {
		t.Errorf("problem = %+v", p)
	}
}

// TestSiteNeedsLaravel: a Blade page that leans on real Laravel (a function
// call, here) cannot become Studio syntax, which has no Laravel behind it.
func TestSiteNeedsLaravel(t *testing.T) {
	src := vfs.Mem{"src/pages/index.blade.php": "<a href=\"{{ route('home') }}\">Home</a>\n"}
	res := Site(src, Studio, render.Options{})
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Message, "needs real Laravel") {
		t.Fatalf("problems = %+v", res.Problems)
	}
	if len(res.Files) != 0 {
		t.Errorf("nothing should be converted")
	}
}

// TestSiteCollision: converting must not write over a file that is already
// there in the other syntax.
func TestSiteCollision(t *testing.T) {
	src := vfs.Mem{
		"src/pages/index.blade.php": "a",
		"src/pages/index.html":      "b",
	}
	res := Site(src, Studio, render.Options{})
	if len(res.Problems) != 1 || res.Problems[0].File != "src/pages/index.html" || !strings.Contains(res.Problems[0].Message, "already exists") {
		t.Fatalf("problems = %+v", res.Problems)
	}
}

// TestSiteNothingToDo: a site already in the target syntax converts to nothing,
// without complaint.
func TestSiteNothingToDo(t *testing.T) {
	res := Site(vfs.Mem{"src/pages/index.html": "<p>hi</p>"}, Studio, render.Options{})
	if len(res.Problems) != 0 || len(res.Files) != 0 || len(res.Removed) != 0 {
		t.Errorf("result = %+v", res)
	}
}

// TestSiteMixed: an .html page may use a .blade.php component. Converting to
// Blade turns the page and leaves the component as it is.
func TestSiteMixed(t *testing.T) {
	src := vfs.Mem{
		"src/pages/index.html":            "<Card title=\"Hi\">x</Card>\n",
		"src/components/card.blade.php":   "@props(['title'])\n<div>{{ $title }}: {{ $slot }}</div>\n",
		"src/components/unused/note.html": "<p><Slot /></p>\n",
	}
	res := Site(src, Blade, render.Options{})
	if len(res.Problems) != 0 {
		t.Fatalf("problems = %+v", res.Problems)
	}
	if got := res.Files["src/pages/index.blade.php"]; got != "<x-card title=\"Hi\">x</x-card>\n" {
		t.Errorf("page = %q", got)
	}
	if got := res.Files["src/components/unused/note.blade.php"]; got != "<p>{{ $slot }}</p>\n" {
		t.Errorf("unused component = %q", got)
	}
	if len(res.Removed) != 2 {
		t.Errorf("removed = %v", res.Removed)
	}
}

// TestParseSyntax: a syntax is asked for by name, in any case. "html" is the
// Studio syntax's older name — and its file extension — and stays accepted.
func TestParseSyntax(t *testing.T) {
	for name, want := range map[string]Syntax{
		"blade": Blade, "Blade": Blade, "BLADE": Blade,
		"studio": Studio, "Studio": Studio, "html": Studio, "HTML": Studio,
	} {
		if got, ok := ParseSyntax(name); !ok || got != want {
			t.Errorf("ParseSyntax(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "twig", "static", " studio"} {
		if got, ok := ParseSyntax(name); ok {
			t.Errorf("ParseSyntax(%q) = %q; want it refused", name, got)
		}
	}
}

// TestSyntaxNames: what each syntax is called in a message, and the extension
// its files carry.
func TestSyntaxNames(t *testing.T) {
	if got := Blade.Name(); got != "Blade" {
		t.Errorf("Blade.Name() = %q", got)
	}
	if got := Studio.Name(); got != "the DevDojo Studio syntax" {
		t.Errorf("Studio.Name() = %q", got)
	}
	if Blade.Ext() != ".blade.php" || Studio.Ext() != ".html" || Syntax("html").Ext() != "" {
		t.Errorf("extensions: %q, %q, %q", Blade.Ext(), Studio.Ext(), Syntax("html").Ext())
	}
}

// TestSiteUnknownSyntax: a syntax nobody has heard of converts nothing and
// says which two exist.
func TestSiteUnknownSyntax(t *testing.T) {
	res := Site(vfs.Mem{"src/pages/index.blade.php": "x"}, Syntax("twig"), render.Options{})
	if len(res.Problems) != 1 || res.Problems[0].Message != `unknown syntax "twig": convert to "blade" or "studio"` {
		t.Fatalf("problems = %+v", res.Problems)
	}
}
