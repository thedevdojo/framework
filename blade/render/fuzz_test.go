package render

import (
	"testing"

	"github.com/thedevdojo/pocketknife/vfs"
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

	entry := "resources/views/pages/index.blade.html"
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
