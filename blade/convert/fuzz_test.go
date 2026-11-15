package convert

import (
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/htmlscan"
	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/blade/scanner"
	"github.com/thedevdojo/pocketknife/vfs"
)

// fuzzData is what a fuzzed page is rendered with: enough for loops, paths
// and comparisons to take both branches.
var fuzzData = map[string]any{
	"a": 1, "b": "x", "on": true, "off": false, "name": "Ada & Bob",
	"items": []any{"p", "q", "r"},
	"post":  map[string]any{"title": "T", "featured": true, "tags": []any{"go", "php"}},
}

// fuzzComponents are there for a fuzzed page's tags to land on.
var fuzzComponents = map[string][2]string{
	"card":         {"@props(['title' => 'T'])\n<i {{ $attributes }}>{{ $title }}{{ $slot }}</i>", "<Props title=\"T\" />\n<i {{ attributes }}>{{ title }}<Slot /></i>"},
	"layouts/main": {"<main>{{ $head ?? '' }}{{ $slot }}</main>", "<main><Slot name=\"head\" /><Slot /></main>"},
}

// FuzzFile holds the converter to its one promise, on arbitrary input: when it
// says a file converted, the result renders the same bytes as the original. It
// may refuse anything it likes. It must never panic, and what it hands back
// must be clean in the other syntax — no tag the scanner could not read.
func FuzzFile(f *testing.F) {
	seeds := []string{
		"", "plain <b>html</b>", "{{ $a }} {!! $b !!} {{-- c --}}", "{{ a }} {!! b !!}",
		"@if ($a == 1)\nA\n@elseif ($on)\nB\n@else\nC\n@endif\n", "<If a=\"1\">\nA\n<Else>\nB\n</If>\n",
		"@foreach ($items as $i => $x)\n{{ $loop->index }}{{ $x }}\n@continue($loop->first)\n@break\n@endforeach",
		"<Foreach items=\"x\" limit=\"2\">\n{{ x }}\n</Foreach>", "<Foreach items=\"i, x\"><Break loop.first /></Foreach>",
		"<x-card :title=\"'Hi ' . $name\" class=\"a\" wide>x</x-card>", "<Card title=\"Hi {{ name }}\" @click.prevent=\"go\" />",
		"<x-layouts.main><x-slot:head>H</x-slot>B</x-layouts.main>", "<Layout src=\"main.html\"><Slot name=\"head\">H</Slot>B</Layout>",
		"@props(['a' => 'b', 'c'])\n{{ $a }}", "<Props a=\"can't &quot;\" :n=\"3\" c />", "@vite('x.css')", "<Vite src=\"a.css, b.css\" />",
		"<div @class(['a', 'b' => $on])>", "<div {{ class(['a', 'b' => on]) }}>",
		"@@click @{{ x }} @media (a) hi@x.y @@ @", "<x-a> </x-a> <?xml ?> {{{ a }}} {{-- {!! @{!! a !!}",
		"<script>if (a<B) { <Card /> }</script><!-- <If a> -->", "<!-- {{ $a }} -->", "@else(x) @endif @break(2) @auth",
		"<Else>more</If><Else>\n (x)</Foreach></Layout><Slot />", "@{{ a @if b }} @@@if @@1 @{!! a !!}",
		// What the tidy print takes back out: hatches, separators, a limit's @break.
		"{!! '@' !!}media (a) {!! '<' !!}Card> {!! '{' !!}@{{ a }}} {!! '@' !!}@{{ a }} {!! '@' !!}{{ $a }}",
		"@if ($on){{ '' }}\nx{{ '' }}@else{{ '' }}y\n{{ '' }}(z)@endif{{ '' }}{{-- c --}}\n<x-card />{{ '' }}\nw",
		"@foreach ($items as $x)\n@break($loop->index >= 2)\n{{ $x }}{{ $loop->iteration }}\n@endforeach",
		"@foreach ($items as $x)@break($loop->index >= $a){{ $x }}@endforeach @foreach ($items as $x)@break($loop->index >= 1){{ $loop->last }}@endforeach",
		"<x-card :title=\"'#' . $a . ' ' . ($post->title ?? 'x')\" :data-id=\"'' . $a\" />",
	}
	for _, s := range seeds {
		f.Add(s, true)
		f.Add(s, false)
	}
	exists := func(base string) bool { _, ok := fuzzComponents[base]; return ok }

	f.Fuzz(func(t *testing.T, src string, toHTML bool) {
		from, to := HTML, Blade
		if toHTML {
			from, to = Blade, HTML
		}
		path := "resources/views/pages/index" + from.Ext()
		out, problems := File(path, []byte(src), to, exists)
		if len(problems) > 0 {
			return // refusing is always allowed
		}

		newPath := "resources/views/pages/index" + to.Ext()
		var toks []scanner.Token
		if to == HTML {
			toks = htmlscan.Scan(newPath, []byte(out))
		} else {
			toks = scanner.Scan(newPath, []byte(out))
		}
		for _, tok := range toks {
			if tok.Kind == scanner.Error {
				t.Fatalf("converted file has a tag the scanner refuses (%s)\n in:  %q\n out: %q", tok.Text, src, out)
			}
		}

		render1 := fuzzRender(path, src, from)
		render2 := fuzzRender(newPath, out, to)
		if render1 != render2 {
			t.Fatalf("renders differently after converting to %s\n in:  %q\n out: %q\n was: %q\n now: %q", to, src, out, render1, render2)
		}
	})
}

// fuzzRender renders one page, with the fuzz components in the given syntax
// beside it, and returns the output.
func fuzzRender(path, src string, syntax Syntax) string {
	site := vfs.Mem{path: src}
	for base, bodies := range fuzzComponents {
		body := bodies[0]
		if syntax == HTML {
			body = bodies[1]
		}
		site["resources/views/components/"+base+syntax.Ext()] = body
	}
	res := render.Compile(site, []string{path}, render.Options{Data: fuzzData})
	var b strings.Builder
	for _, body := range res.Files {
		b.WriteString(body)
	}
	return b.String()
}
