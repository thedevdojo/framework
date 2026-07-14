package parser

import (
	"testing"

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
