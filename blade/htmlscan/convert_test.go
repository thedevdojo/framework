package htmlscan

import (
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade/scanner"
)

// TestScanClassAndStyle: {{ class([...]) }} and {{ style([...]) }} are the HTML
// spelling of Blade's @class and @style — the one pair of directives that sit
// inside a plain HTML tag, where a capitalized tag cannot go.
func TestScanClassAndStyle(t *testing.T) {
	cases := []struct{ html, blade string }{
		{`<a {{ class(['btn', 'on' => active]) }}>x</a>`, `<a @class(['btn', 'on' => $active])>x</a>`},
		{`<a {{ style(['color: red' => post.hot]) }}>x</a>`, `<a @style(['color: red' => $post->hot])>x</a>`},
		{"<a {{ class([\n  'btn',\n  'on' => loop.first,\n]) }}>x</a>", "<a @class([\n  'btn',\n  'on' => $loop->first,\n])>x</a>"},
	}
	for _, tc := range cases {
		got := shape(Scan("page.html", []byte(tc.html)))
		want := shape(scanner.Scan("page.html", []byte(tc.blade)))
		if got != want {
			t.Errorf("html  %s\nblade %s\n--- html tokens ---\n%s--- blade tokens ---\n%s", tc.html, tc.blade, got, want)
		}
	}

	// Anything that is not exactly class(...) or style(...) is an ordinary echo.
	for _, src := range []string{`{{ class }}`, `{{ classes(['a']) }}`, `{{ class(['a']) . 'x' }}`, `{{ post.class(['a']) }}`, `{!! class(['a']) !!}`} {
		toks := Scan("page.html", []byte(src))
		if len(toks) != 1 || toks[0].Kind == scanner.Directive {
			t.Errorf("Scan(%q) = %s; want one echo", src, shape(toks))
		}
	}
}

// TestFriendly: a Blade expression is rewritten the HTML way only when Expr
// reads it straight back; otherwise it is left in Blade form, which an .html
// file accepts as it is.
func TestFriendly(t *testing.T) {
	cases := []struct{ blade, want string }{
		{`$title`, `title`},
		{`$post->title`, `post.title`},
		{`$post->author->name ?? 'Anon'`, `post.author.name ?? 'Anon'`},
		{`$posts[0]->title`, `posts[0].title`},
		{`$a == 'x' && !$b`, `a == 'x' and !b`},
		{`$a || $b`, `a or b`},
		{`$a&&$b`, `a&&b`},
		{`$loop->iteration == 1`, `loop.iteration == 1`},
		{`$attributes->merge(['class' => 'btn', 'id' => $id])`, `attributes.merge(['class' => 'btn', 'id' => id])`},
		{`['a', 'b' => $post->on]`, `['a', 'b' => post.on]`},
		{`'$not->touched' == "$a->b"`, `'$not->touched' == "$a->b"`},
		{`true`, `true`}, {`3`, `3`}, {`'x'`, `'x'`}, {``, ``},
		// Names the Studio syntax reads as words are left in Blade form.
		{`$and`, `$and`}, {`$not->x`, `$not->x`}, {`$true`, `$true`}, {`$a == $or`, `$a == $or`},
		// Spaced member access is not something a dot can say.
		{`$a -> b`, `a -> b`},
	}
	for _, tc := range cases {
		got := Friendly(tc.blade)
		if got != tc.want {
			t.Errorf("Friendly(%q) = %q; want %q", tc.blade, got, tc.want)
		}
		if back := Expr(got); back != tc.blade {
			t.Errorf("Expr(Friendly(%q)) = %q; the round trip must be exact", tc.blade, back)
		}
	}
}

// TestLiteral: text written through Literal scans back as that exact text —
// whatever it contains — and Literal leaves the context where the scanner is
// at its end. pre is text read first, to set the context up.
func TestLiteral(t *testing.T) {
	cases := []struct{ name, pre, text, want, elemAfter string }{
		{"plain html", "", `<div class="a">x</div>`, `<div class="a">x</div>`, ""},
		{"at signs are plain", "", `@click="x" @media (a) a@b.co`, `@click="x" @media (a) a@b.co`, ""},
		{"a value", "", `{title}`, `\{title}`, ""},
		{"not a value", "", `{ open: false } {} a { b c }`, `{ open: false } {} a { b c }`, ""},
		{"a backslash before a brace", "", `\{x} \{ a\b`, `\\{x} \\{ a\b`, ""},
		{"braces", "", `{{ x }}`, `\{\{ x }}`, ""},
		{"spaced braces", "", `{{x y}}`, `\{{x y}}`, ""},
		{"unclosed braces", "", `a {{ b`, `a \{{ b`, ""},
		{"raw braces", "", `{!! x !!}`, `\{!! x !!}`, ""},
		{"comment braces", "", `{{-- x --}}`, `\{{-- x --}}`, ""},
		{"an at-sign before braces", "", `@{{ x }}`, `@\{\{ x }}`, ""},
		{"an at-sign before unclosed braces", "", `@{{ x`, `@\{{ x`, ""},
		{"a value in an attribute", "", `<a title="{x}" href={y}>`, `<a title="\{x}" href=\{y}>`, ""},
		{"a code attribute", "", `<a x-text="{x}" onclick="{y}" :class="{z}">`, `<a x-text="{x}" onclick="{y}" :class="{z}">`, ""},
		{"braces in a code attribute", "", `<a x-text="{{ x }}">`, `<a x-text="@{{ x }}">`, ""},
		{"component tag", "", `<Button>x</Button>`, `{!! '<' !!}Button>x{!! '<' !!}/Button>`, ""},
		{"built-in tag", "", `<If a>`, `{!! '<' !!}If a>`, ""},
		{"static spellings", "", `<include src="a">`, `{!! '<' !!}include src="a">`, ""},
		{"shouted html", "", `<BR><TABLE>`, `<BR><TABLE>`, ""},
		{"html comment is already opaque", "", `<!-- <If a> {{ x }} {y} -->`, `<!-- <If a> {{ x }} {y} -->`, ""},
		{"comment left open", "", `<!-- start`, `{!! '<' !!}!-- start`, ""},
		{"enters script", "", `<script>a<B>c{x}`, `<script>a<B>c{x}`, "script"},
		{"braces inside script", "<script>", `{x} {{ x }}`, `{x} @{{ x }}`, "script"},
		{"built-in inside script", "<script>", `a<If x>b`, `a{!! '<' !!}If x>b`, "script"},
		{"leaves script", "<script>", `x</script><Button />{x}`, `x</script>{!! '<' !!}Button />\{x}`, ""},
		{"style does not close script", "<script>", `</style>{x}`, `</style>{x}`, "script"},
		{"comment marker inside script", "<script>", `<!-- a<B>`, `<!-- a<B>`, "script"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ctx Context
			Literal(tc.pre, &ctx)
			got := Literal(tc.text, &ctx)
			if got != tc.want || ctx.Elem() != tc.elemAfter {
				t.Errorf("Literal(%q) after %q = %q, %q; want %q, %q", tc.text, tc.pre, got, ctx.Elem(), tc.want, tc.elemAfter)
			}
			// And the point of it: scanned, it prints the text it was given.
			if printed := printedText(t, tc.pre+got); printed != tc.pre+tc.text {
				t.Errorf("Literal(%q) = %q prints %q", tc.text, got, printed)
			}
		})
	}
}

// printedText is what src prints when every token in it is text or a hatch.
func printedText(t *testing.T, src string) string {
	t.Helper()
	var printed strings.Builder
	for _, tok := range Scan("x.html", []byte(src)) {
		switch tok.Kind {
		case scanner.Text:
			printed.WriteString(tok.Text)
		case scanner.RawEcho:
			s := strings.TrimSuffix(strings.TrimPrefix(tok.Text, "'"), "'")
			printed.WriteString(strings.TrimPrefix(s, `\`))
		default:
			t.Fatalf("%q scans to a %v token", src, tok.Kind)
		}
	}
	return printed.String()
}

// TestIsComponentTag: only a name the scanner reads as a component tag is one.
func TestIsComponentTag(t *testing.T) {
	for name, want := range map[string]bool{
		"Button": true, "SectionsHero": true, "Sections.Hero": true, "Div": true,
		"If": false, "Layout": false, "Slot": false, "Script": false, "button": false,
		"A": false, "BR": false, "H1": false, "": false,
	} {
		if got := IsComponentTag(name); got != want {
			t.Errorf("IsComponentTag(%q) = %v; want %v", name, got, want)
		}
	}
}
