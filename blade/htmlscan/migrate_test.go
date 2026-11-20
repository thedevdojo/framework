package htmlscan

import (
	"testing"
)

// TestMigrate: an .html view written for double braces is rewritten to single
// ones, and reads today exactly as it read then.
func TestMigrate(t *testing.T) {
	cases := []struct {
		name, in, want string
		stats          MigrateStats
	}{
		{"a value", `<h2>{{ post.title }}</h2>`, `<h2>{post.title}</h2>`, MigrateStats{Values: 1}},
		{"unspaced and spaced", `{{x}} {{  a ?? 'b'  }}`, `{x} {a ?? 'b'}`, MigrateStats{Values: 2}},
		{"in attributes", `<a href="{{ u }}" class="btn btn-{{ v }}" style="height: {{ h }}px">`,
			`<a href="{u}" class="btn btn-{v}" style="height: {h}px">`, MigrateStats{Values: 3}},
		{"a class call", `<a {{ class(['a' => on]) }}>`, `<a {class(['a' => on])}>`, MigrateStats{Values: 1}},
		{"raw and comments stay", `{!! body !!}{{-- note --}}`, `{!! body !!}{{-- note --}}`, MigrateStats{}},
		{"where a brace is not read", `<script>a = {{ x }}; b = {c};</script><i x-text="{{ y }}" :class="{ on: z }"></i><!-- {{ w }} {v} -->`,
			`<script>a = {{ x }}; b = {c};</script><i x-text="{{ y }}" :class="{ on: z }"></i><!-- {{ w }} {v} -->`, MigrateStats{}},
		{"what a single brace cannot say", `{{ a ?? '}' }} {{ now() }}`, `{{ a ?? '}' }} {{ now() }}`, MigrateStats{Kept: 2}},
		{"the old escape", `@{{ x }} and @{{ a b }}`, `\{\{ x }} and \{{ a b }}`, MigrateStats{Escapes: 2, Braces: 3}},
		{"the old escape where a brace is not read", `<script>@{{ x }}</script>`, `<script>@{{ x }}</script>`, MigrateStats{}},
		{"text that would open a value", `Write {title} or {post.title}; { open: false } and {} stay.`,
			`Write \{title} or \{post.title}; { open: false } and {} stay.`, MigrateStats{Braces: 2}},
		{"a backslash before a brace", `a\{b} c\{ d`, `a\\{b} c\\{ d`, MigrateStats{Braces: 2}},
		{"a backslash before a value", `a\{{ x }}`, `a{!! '\\' !!}{x}`, MigrateStats{Values: 1}},
		{"openers left open", `{{ a {!! b`, `{{ a {!! b`, MigrateStats{}},
		{"a component attribute", `<Card title="Hi {{ name }}" note="{x} {{ y }}" x-text="{{ a }}" :n="m" />`,
			`<Card title="Hi {name}" note="\{x} {y}" x-text="{{ a }}" :n="m" />`, MigrateStats{Values: 2, Braces: 1}},
		{"a layout's src is a path", `<Layout src="main.html" title="{{ t }}">x</Layout>`, `<Layout src="main.html" title="{t}">x</Layout>`, MigrateStats{Values: 1}},
		{"nothing to do", `<p>Hello</p>`, `<p>Hello</p>`, MigrateStats{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, stats, err := Migrate("page.html", []byte(tc.in))
			if err != nil {
				t.Fatalf("Migrate(%q): %v", tc.in, err)
			}
			if string(got) != tc.want || stats != tc.stats {
				t.Errorf("Migrate(%q) =\n  %s %+v\nwant\n  %s %+v", tc.in, got, stats, tc.want, tc.stats)
			}
		})
	}
}

// TestMigrateRefuses: a backslash right before {{ }} in a component's
// attribute has no exact single-brace spelling.
func TestMigrateRefuses(t *testing.T) {
	src := []byte(`<Card title="a\{{ x }}" />`)
	got, _, err := Migrate("page.html", src)
	if err == nil {
		t.Fatalf("Migrate(%q) = %q, want an error", src, got)
	}
	if string(got) != string(src) {
		t.Errorf("a refused file must come back as it was, got %q", got)
	}
}

// FuzzMigrate: whatever the file, Migrate either refuses or returns a file
// that reads today as the original read before.
func FuzzMigrate(f *testing.F) {
	for _, seed := range []string{
		`<h2>{{ post.title }}</h2> {title} @{{ raw }} \{x} <a href="{{ u }}" x-text="{{ v }}">`,
		`<Card title="Hi {{ n }} {x}" /><Layout src="a.html" t="{{ t }}">x</Layout>`,
		`<script>{{ a }} {b}</script><!-- {{ c }} {d} --><style>{e}</style>{f}`,
		`a\{{ x }} {{ a ?? '}' }} {!! r !!} {{-- c --}} {{ a {!! b`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		out, _, err := Migrate("page.html", []byte(src))
		if err != nil {
			if string(out) != src {
				t.Fatalf("refused, but changed the file")
			}
			return
		}
		old, _ := ScanWith("page.html", []byte(src), Options{Legacy: true})
		now, info := ScanWith("page.html", out, Options{})
		if !sameTokens(old, now) {
			t.Fatalf("Migrate(%q) = %q reads differently", src, out)
		}
		if len(info.DoubleBraces) > 0 {
			t.Fatalf("Migrate(%q) = %q leaves double braces a single brace would do", src, out)
		}
	})
}
