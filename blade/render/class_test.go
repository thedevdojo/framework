package render

import (
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

// @class / @style — Laravel's conditional class/style directives. Each
// compiles to `class="…"` / `style="…"` built by Arr::toCssClasses /
// Arr::toCssStyles: a numeric-key entry always contributes its value, a
// string-key entry contributes the key when its value is truthy. Like every
// other compiled directive they eat one following newline.

func TestClassDirective(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: `<span @class(['p-4', 'font-bold' => $active, 'hidden' => $off])>x</span>`}
	res := Compile(fs, []string{entry}, Options{Data: map[string]any{"active": true, "off": false}})
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", res.Diagnostics)
	}
	want := `<span class="p-4 font-bold">x</span>`
	if got := res.Files["index.html"]; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}

func TestClassDirectiveVariableEntry(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: `<i @class([$cls, 'b' => 1])></i>`}
	res := Compile(fs, []string{entry}, Options{Data: map[string]any{"cls": "a"}})
	want := `<i class="a b"></i>`
	if got := res.Files["index.html"]; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}

func TestClassDirectiveEmpty(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: `<i @class([])></i>`}
	res := Compile(fs, []string{entry}, Options{})
	if got, want := res.Files["index.html"], `<i class=""></i>`; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}

// A condition that fails to evaluate is diagnosed and the whole directive
// renders nothing (real Blade would have fataled mid-echo).
func TestClassDirectiveEvalError(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: `<i @class(['a' => $nope])></i>`}
	res := Compile(fs, []string{entry}, Options{})
	if got, want := res.Files["index.html"], `<i ></i>`; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Construct != "undefined-variable" {
		t.Fatalf("diagnostics = %v; want one undefined-variable", res.Diagnostics)
	}
}

func TestClassDirectiveEatsNewline(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: "<i @class(['a'])\n></i>"}
	res := Compile(fs, []string{entry}, Options{})
	if got, want := res.Files["index.html"], `<i class="a"></i>`; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}

func TestStyleDirective(t *testing.T) {
	entry := "resources/views/pages/index.blade.php"
	fs := vfs.Mem{entry: `<i @style(['color: red', 'font-weight: bold' => $b, 'display: none' => $off])></i>`}
	res := Compile(fs, []string{entry}, Options{Data: map[string]any{"b": true, "off": false}})
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", res.Diagnostics)
	}
	want := `<i style="color: red; font-weight: bold;"></i>`
	if got := res.Files["index.html"]; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}

// @class inside a COMPONENT file, driven by the component's own props.
func TestClassDirectiveInComponent(t *testing.T) {
	res := compileMem(t, map[string]string{
		pageEntry: `<x-badge :on="true"/>`,
		"resources/views/components/badge.blade.php": `@props(['on' => false])<b @class(['lit' => $on])>!</b>`,
	}, nil)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", res.Diagnostics)
	}
	if got, want := res.Files["index.html"], `<b class="lit">!</b>`; got != want {
		t.Errorf("output = %q; want %q", got, want)
	}
}
