package scanner

import "testing"

func FuzzScan(f *testing.F) {
	seeds := []string{
		"{{ $x }}",
		"@if($a)x@endif",
		"<x-a b='c'>",
		"@{{",
		"{{--",
		"@foreach(",
		"</x-slot>",
		"<x-slot:head class='p-4'>",
		"@@if",
		"{!! $h !!}",
		"line1\n{{ $x }}\n<x-btn label=\"a > b\"/>",
		"@if ($a == 'x)')",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks := Scan("f.blade.html", []byte(src)) // must not panic
		// Reconstruction invariant: total consumed bytes == len(src) is NOT
		// guaranteed (escapes rewrite), but token positions must be ordered.
		last := Pos{Line: 1, Col: 0}
		for _, tk := range toks {
			if tk.Pos.Line < last.Line || (tk.Pos.Line == last.Line && tk.Pos.Col <= last.Col) {
				t.Fatalf("positions not monotonic: %+v after %+v", tk.Pos, last)
			}
			last = tk.Pos
			// Src is populated for Directive tokens only, always begins at the
			// '@', and is empty for every other kind.
			if tk.Kind == Directive {
				if len(tk.Src) == 0 || tk.Src[0] != '@' {
					t.Fatalf("directive Src = %q; want a non-empty slice starting with '@'", tk.Src)
				}
			} else if tk.Src != "" {
				t.Fatalf("non-directive %v has Src %q; want empty", tk.Kind, tk.Src)
			}
		}
	})
}
