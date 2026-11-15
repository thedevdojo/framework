package parser

import (
	"testing"

	"github.com/thedevdojo/pocketknife/blade/htmlscan"
	"github.com/thedevdojo/pocketknife/blade/scanner"
)

// FuzzParse asserts the parser never panics on arbitrary input. Diags may be
// anything; the only invariant is that Parse(…, Scan(…)) always returns.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"@if ($a)A@elseif ($b)B@else C@endif",
		"@foreach ($rows as $row){{ $row }}@endforeach",
		"@foreach ($rows as $k => $v)@break($v)@continue@endforeach",
		"<x-layouts.main title=\"Home\"><x-slot:head><meta></x-slot>body</x-layouts.main>",
		"<x-a><x-a><x-a>x</x-a></x-a></x-a>",
		"@endif",
		"</x-slot>",
		"</x-card>",
		"@foreach ($rows)x@endforeach",
		"@foreach ( as $r)@endforeach",
		"<x-c><x-slot:a>1</x-slot><x-slot:a>2</x-slot></x-c>",
		"@if ($a)@foreach ($x as $y)@else@endforeach@endif",
		"@auth secret @endauth",
		"@props(['title' => 'x'])@vite(['a.js'])",
		"{{-- c --}}{{ $x }}{!! $h !!}",
		"@if (@foreach (<x-",
		"@foreach ($a as $b => $c => $d)@endforeach",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		// Must not panic; must always return.
		_, _ = Parse("f", scanner.Scan("f", []byte(src)))
	})
}

// FuzzParseHTML is FuzzParse for the HTML syntax: whatever the HTML scanner
// makes of arbitrary bytes, the parser must take it without panicking.
func FuzzParseHTML(f *testing.F) {
	seeds := []string{
		`<If condition="a">A<ElseIf condition="b">B<Else>C</If>`,
		`<Foreach rows="row">{{ row }}</Foreach>`,
		`<Foreach rows="k, v"><Break condition="v" /><Continue /></Foreach>`,
		`<Layout src="main.html" title="Home"><Slot name="head"><meta></Slot>body</Layout>`,
		`<A><A><A>x</A></A></A>`,
		`</If>`, `</Slot>`, `</Card>`, `<Else>`, `<ElseIf>`, `<Break />`,
		`<Foreach rows>x</Foreach>`, `<Foreach ="r"></Foreach>`,
		`<C><Slot name="a">1</Slot><Slot name="a">2</Slot></C>`,
		`<If condition="a"><Foreach x="y"><Else></Foreach></If>`,
		`<Props title="x" /><Vite src="a.js" />`,
		`{{-- c --}}{{ x }}{!! h !!}<Slot /><Slot name="t" />`,
		`<If <Foreach <Card`, `<If></If>`, `<Layout>x</Layout>`, `<include>`,
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		// Must not panic; must always return.
		_, _ = Parse("f.html", htmlscan.Scan("f.html", []byte(src)))
	})
}
