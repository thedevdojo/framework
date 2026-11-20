package render

import (
	"strings"
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

// FuzzCompile asserts Compile never panics on arbitrary page bytes, exercising
// the FULL pipeline (scanner → parser → renderer) as one page — including the
// Task 11 diagnostics paths: Unknown/app-tier classification, the
// `$attributes` carve-out, parse-diag folding, and dedupe. Compile is designed
// to always return (see the package doc); the only invariant here is that it
// does, on any input and against a small fixed data map (so echoes/conditions/
// loops referencing common names sometimes succeed instead of every path being
// an immediate undefined-variable).
func FuzzCompile(f *testing.F) {
	seeds := []string{
		"{{ $x }}", "@if($a)x@endif", "<x-a b='c'>", "@{{", "{{--",
		"@foreach(", "</x-slot>", "<x-slot:head class='p-4'>", "@@if",
		"{!! $h !!}", "line1\n{{ $x }}\n<x-btn label=\"a > b\"/>", "@if ($a == 'x)')",
		"@if ($a)A@elseif ($b)B@else C@endif",
		"@foreach ($rows as $row){{ $row }}@endforeach",
		"@foreach ($rows as $k => $v)@break($v)@continue@endforeach",
		"<x-layouts.main title=\"Home\"><x-slot:head><meta></x-slot>body</x-layouts.main>",
		"<x-a><x-a><x-a>x</x-a></x-a></x-a>",
		"@endif", "</x-card>", "@foreach ($rows)x@endforeach", "@foreach ( as $r)@endforeach",
		"<x-c><x-slot:a>1</x-slot><x-slot:a>2</x-slot></x-c>",
		"@if ($a)@foreach ($x as $y)@else@endforeach@endif",
		"@auth secret @endauth", "@props(['title' => 'x'])@vite(['a.js'])",
		"{{-- c --}}{{ $x }}{!! $h !!}", "@if (@foreach (<x-",
		"@foreach ($a as $b => $c => $d)@endforeach", "",
		"{{ }}", "{{ ]]] }}", "{!! !!}", "@if()@endif", "{{ $a->b->c }}",
		// Task 11-specific: Unknown directives, the $attributes carve-out, a
		// stray closer (parse diag), a self-recursing component, duplicate
		// entries-shaped content that could hit the dedupe path.
		"@auth\nsecret\n@endauth", "@endauth", "@php echo 1; @endphp", "@csrf",
		"@include('x')", "@guest @endguest",
		"{{ $attributes }}", "@if ($attributes)x@endif",
		"<x-c :label=\"$attributes\"/>",
		"A@endif B", "@elseif(1)", "@else",
		"<x-recur/>",
		"<x-c/><x-c/>",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	entry := "resources/views/pages/index.blade.php"
	data := map[string]any{
		"a": true, "b": false, "x": "v", "h": "<b>x</b>",
		"rows": []any{"a", "b"}, "n": float64(2),
	}

	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Compile panicked on %q: %v", src, r)
			}
		}()
		_ = Compile(vfs.Mem{entry: src}, []string{entry}, Options{Data: data})
	})
}

// FuzzCompileHTML is FuzzCompile for the Studio syntax: the same full pipeline,
// entered through an .html page, with one .html and one .blade.php component on
// disk so capitalized tags resolve down both paths. One more invariant holds
// here that Blade cannot offer: an .html page never reports a Blade directive,
// because in an .html file "@" is plain text.
func FuzzCompileHTML(f *testing.F) {
	seeds := []string{
		`{{ x }}`, `<If condition="a">x</If>`, `<If a>x<Else>y</If>`, `<If x="v">x</If>`,
		`<If :n="2">x</If>`, `<If not="b">x</If>`, `<If>`, `</If>`, `<Else>`,
		`<Foreach rows="row">{{ row }}{{ loop.index }}</Foreach>`,
		`<Foreach rows="k, v" limit="1"><Break condition="v" /><Continue /></Foreach>`,
		`<Foreach collection="rows" as="r" count="n">{{ r }}</Foreach>`,
		`<Foreach rows="row" limit="x">a</Foreach>`, `<Foreach rows>`, `</Foreach>`,
		`<Layout src="main.html" title="Home"><Slot name="head"><meta></Slot>body</Layout>`,
		`<include src="card.html"></include>`, `<Include src="../x" />`, `<Layout>`,
		`<Card title="Hi {{ x }}" :n="n" class="a">body</Card>`, `<Card/><Card/>`,
		`<Old label="{{ x }}" />`, `<Missing />`, `<Sections.Hero />`, `<SectionsHeroBigOne />`,
		`<Card><Slot name="a">1</Slot><Slot name="a">2</Slot></Card>`,
		`<Slot />`, `<Slot name="t" />`, `<Slot name="t">x</Slot>`,
		`<Props title="x" :n="3" />`, `<Vite src="a.js, b.css" />`,
		`{{-- c --}}{{ x }}{!! h !!}@{{ raw }}`, `{{ }}`, `{{ ]]] }}`, `{{ a.b.c }}`,
		`{{ attributes }}`, `{{ route('home') }}`, `{{ a = b and not c }}`,
		`@if($a)x@endif @foreach @auth @csrf a@b.co`,
		`<script>a<B>c {{ x }}</script><style>@media{}</style><!-- <If> -->`,
		`<Recur />`, "",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	entry := "resources/views/pages/index.html"
	data := map[string]any{
		"a": true, "b": false, "x": "v", "h": "<b>x</b>",
		"rows": []any{"a", "b"}, "n": float64(2),
	}

	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Compile panicked on %q: %v", src, r)
			}
		}()
		res := Compile(vfs.Mem{
			entry:                                  src,
			"resources/views/components/card.html": `<Props title="T" /><div {{ attributes }}>{{ title }}<Slot /></div>`,
			"resources/views/components/old.blade.php":          `@props(['label' => 'L'])<b>{{ $label }}</b>`,
			"resources/views/components/recur.html":             `<Recur />`,
			"resources/views/components/layouts/main.html":      `<html>{{ slot }}</html>`,
			"resources/views/components/sections/hero.html":     `<h1><Slot /></h1>`,
			"resources/views/components/sections-hero.html":     `<h2></h2>`,
			"resources/views/components/sections/hero/big.html": src,
		}, []string{entry}, Options{Data: data})
		for _, d := range res.Diagnostics {
			if strings.HasSuffix(d.File, ".html") && strings.HasPrefix(d.Construct, "@") {
				t.Fatalf("an .html file reported a Blade directive %q (%s) for %q", d.Construct, d.Message, src)
			}
		}
	})
}
