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
// whatever it contains — and Literal reports the raw-text element the scanner
// is left inside.
func TestLiteral(t *testing.T) {
	cases := []struct{ name, text, raw, want, rawAfter string }{
		{"plain html", `<div class="a">x</div>`, "", `<div class="a">x</div>`, ""},
		{"at signs are plain", `@click="x" @media (a) a@b.co`, "", `@click="x" @media (a) a@b.co`, ""},
		{"braces", `{{ x }}`, "", `@{{ x }}`, ""},
		{"unclosed braces", `a {{ b`, "", `a {!! '{' !!}{ b`, ""},
		{"raw braces", `{!! x !!}`, "", `{!! '{' !!}!! x !!}`, ""},
		{"comment braces", `{{-- x --}}`, "", `{!! '{' !!}{-- x --}}`, ""},
		{"an at-sign before braces", `@{{ x }}`, "", `@@{{ x }}`, ""},
		{"an at-sign before unclosed braces", `@{{ x`, "", `{!! '@' !!}{!! '{' !!}{ x`, ""},
		{"component tag", `<Button>x</Button>`, "", `{!! '<' !!}Button>x{!! '<' !!}/Button>`, ""},
		{"built-in tag", `<If a>`, "", `{!! '<' !!}If a>`, ""},
		{"static spellings", `<include src="a">`, "", `{!! '<' !!}include src="a">`, ""},
		{"shouted html", `<BR><TABLE>`, "", `<BR><TABLE>`, ""},
		{"html comment is already opaque", `<!-- <If a> {{ x }} -->`, "", `<!-- <If a> {{ x }} -->`, ""},
		{"comment left open", `<!-- start`, "", `{!! '<' !!}!-- start`, ""},
		{"enters script", `<script>a<B>c`, "", `<script>a<B>c`, "script"},
		{"built-in inside script", `a<If x>b`, "script", `a{!! '<' !!}If x>b`, "script"},
		{"leaves script", `x</script><Button />`, "script", `x</script>{!! '<' !!}Button />`, ""},
		{"style does not close script", `</style>`, "script", `</style>`, "script"},
		{"comment marker inside script", `<!-- a<B>`, "script", `<!-- a<B>`, "script"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rawAfter := Literal(tc.text, tc.raw)
			if got != tc.want || rawAfter != tc.rawAfter {
				t.Errorf("Literal(%q, %q) = %q, %q; want %q, %q", tc.text, tc.raw, got, rawAfter, tc.want, tc.rawAfter)
			}
			// And the point of it: scanned, it prints the text it was given.
			prefix := ""
			if tc.raw != "" {
				prefix = "<" + tc.raw + ">"
			}
			var printed strings.Builder
			for _, tok := range Scan("x.html", []byte(prefix+got)) {
				switch tok.Kind {
				case scanner.Text:
					printed.WriteString(tok.Text)
				case scanner.RawEcho:
					printed.WriteString(strings.Trim(tok.Text, "'"))
				default:
					t.Fatalf("Literal(%q) = %q scans to a %v token", tc.text, got, tok.Kind)
				}
			}
			if printed.String() != prefix+tc.text {
				t.Errorf("Literal(%q) = %q prints %q", tc.text, got, printed.String())
			}
		})
	}
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
