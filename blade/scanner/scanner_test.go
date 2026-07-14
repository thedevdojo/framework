package scanner

import (
	"fmt"
	"testing"
)

// at builds an expected position; the golden table only asserts Line/Col.
func at(l, c int) Pos { return Pos{Line: l, Col: c} }

func kindName(k Kind) string {
	switch k {
	case Text:
		return "Text"
	case Echo:
		return "Echo"
	case RawEcho:
		return "RawEcho"
	case Comment:
		return "Comment"
	case Directive:
		return "Directive"
	case TagOpen:
		return "TagOpen"
	case TagClose:
		return "TagClose"
	case SlotOpen:
		return "SlotOpen"
	case SlotClose:
		return "SlotClose"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

func attrsEqual(a, b []Attr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Value != b[i].Value || a[i].Bound != b[i].Bound {
			return false
		}
	}
	return true
}

func fmtAttrs(as []Attr) string {
	if len(as) == 0 {
		return "[]"
	}
	out := "["
	for i, a := range as {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("{n=%q v=%q bound=%v}", a.Name, a.Value, a.Bound)
	}
	return out + "]"
}

func fmtTok(t Token) string {
	return fmt.Sprintf("%s name=%q text=%q self=%v attrs=%s @L%dC%d",
		kindName(t.Kind), t.Name, t.Text, t.SelfClose, fmtAttrs(t.Attrs), t.Pos.Line, t.Pos.Col)
}

// assertTokens compares Kind, Name, Text, SelfClose, Attrs, and Pos.Line/Col.
func assertTokens(t *testing.T, got, want []Token) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("token count: got %d, want %d", len(got), len(want))
		dumpTokens(t, got, want)
		return
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.Name != w.Name || g.Text != w.Text ||
			g.SelfClose != w.SelfClose || !attrsEqual(g.Attrs, w.Attrs) ||
			g.Pos.Line != w.Pos.Line || g.Pos.Col != w.Pos.Col {
			t.Errorf("token[%d] mismatch:\n  got:  %s\n  want: %s", i, fmtTok(g), fmtTok(w))
		}
	}
}

func dumpTokens(t *testing.T, got, want []Token) {
	t.Helper()
	t.Logf("--- got (%d) ---", len(got))
	for i, tk := range got {
		t.Logf("  [%d] %s", i, fmtTok(tk))
	}
	t.Logf("--- want (%d) ---", len(want))
	for i, tk := range want {
		t.Logf("  [%d] %s", i, fmtTok(tk))
	}
}

func TestScanGolden(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []Token
	}{
		{"plain", `<p>hi</p>`, []Token{{Kind: Text, Text: "<p>hi</p>", Pos: at(1, 1)}}},
		{"echo", `a {{ $x }} b`, []Token{
			{Kind: Text, Text: "a ", Pos: at(1, 1)},
			{Kind: Echo, Text: "$x", Pos: at(1, 3)},
			{Kind: Text, Text: " b", Pos: at(1, 11)},
		}},
		{"raw", `{!! $html !!}`, []Token{{Kind: RawEcho, Text: "$html", Pos: at(1, 1)}}},
		{"comment", `x{{-- gone --}}y`, []Token{
			{Kind: Text, Text: "x", Pos: at(1, 1)},
			{Kind: Comment, Pos: at(1, 2)},
			{Kind: Text, Text: "y", Pos: at(1, 16)},
		}},
		{"directive_args_quotes", `@if ($a == 'x)')`, []Token{
			{Kind: Directive, Name: "if", Text: `$a == 'x)'`, Pos: at(1, 1)},
		}},
		{"directive_multiline", "@foreach (\n  $items as $item\n)", []Token{
			{Kind: Directive, Name: "foreach", Text: "$items as $item", Pos: at(1, 1)},
		}},
		{"escape_at", `@@if`, []Token{{Kind: Text, Text: "@if", Pos: at(1, 1)}}},
		{"escape_braces", `@{{ raw }}`, []Token{{Kind: Text, Text: "{{ raw }}", Pos: at(1, 1)}}},
		{"unterminated_echo", `{{ $x`, []Token{{Kind: Text, Text: "{{ $x", Pos: at(1, 1)}}},
		{"xtag", `<x-sections.hero heading="Big &amp; bold" :plans="$plans" wide/>`, []Token{
			{Kind: TagOpen, Name: "sections.hero", SelfClose: true, Pos: at(1, 1), Attrs: []Attr{
				{Name: "heading", Value: "Big &amp; bold"},
				{Name: "plans", Value: "$plans", Bound: true},
				{Name: "wide", Value: ""},
			}},
		}},
		{"xtag_gt_in_attr", `<x-btn label="a > b">go</x-btn>`, []Token{
			{Kind: TagOpen, Name: "btn", Attrs: []Attr{{Name: "label", Value: "a > b"}}, Pos: at(1, 1)},
			{Kind: Text, Text: "go", Pos: at(1, 22)},
			{Kind: TagClose, Name: "btn", Pos: at(1, 24)},
		}},
		{"slot", `<x-slot:title>T</x-slot>`, []Token{
			{Kind: SlotOpen, Name: "title", Pos: at(1, 1)},
			{Kind: Text, Text: "T", Pos: at(1, 15)},
			{Kind: SlotClose, Pos: at(1, 16)},
		}},
		{"newlines_pos", "line1\n{{ $x }}", []Token{
			{Kind: Text, Text: "line1\n", Pos: at(1, 1)},
			{Kind: Echo, Text: "$x", Pos: at(2, 1)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertTokens(t, Scan("t.blade.html", []byte(c.src)), c.want) })
	}
}

// TestScanClarifications covers the authoritative rules the golden table does
// not spell out, plus recovery paths and any fuzz-found regressions.
func TestScanClarifications(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []Token
	}{
		// @@ only strips when followed by an identifier start; otherwise literal.
		{"bare_at_at_eof", `@@`, []Token{{Kind: Text, Text: "@@", Pos: at(1, 1)}}},
		{"bare_at_at_space", `@@ if`, []Token{{Kind: Text, Text: "@@ if", Pos: at(1, 1)}}},
		{"bare_at_at_digit", `@@1x`, []Token{{Kind: Text, Text: "@@1x", Pos: at(1, 1)}}},
		{"at_at_underscore", `@@_x`, []Token{{Kind: Text, Text: "@_x", Pos: at(1, 1)}}},

		// '@' followed by a non-letter is plain Text.
		{"at_space", `@ if`, []Token{{Kind: Text, Text: "@ if", Pos: at(1, 1)}}},
		{"at_digit", `@123`, []Token{{Kind: Text, Text: "@123", Pos: at(1, 1)}}},
		{"at_eof", `@`, []Token{{Kind: Text, Text: "@", Pos: at(1, 1)}}},

		// Directive names lowercase; no-arg directive keeps trailing whitespace
		// as Text (it is not consumed by the directive token).
		{"directive_no_args", "@endif\nx", []Token{
			{Kind: Directive, Name: "endif", Pos: at(1, 1)},
			{Kind: Text, Text: "\nx", Pos: at(1, 7)},
		}},
		{"directive_uppercased", `@If($a)`, []Token{
			{Kind: Directive, Name: "if", Text: "$a", Pos: at(1, 1)},
		}},

		// Unterminated directive args -> the whole run from '@' is Text.
		{"directive_unterminated", `@foreach($x`, []Token{
			{Kind: Text, Text: "@foreach($x", Pos: at(1, 1)},
		}},
		{"directive_open_paren_eof", `@if(`, []Token{
			{Kind: Text, Text: "@if(", Pos: at(1, 1)},
		}},

		// Unterminated <x- tag -> the whole run from '<' is Text (EOF before '>').
		{"xtag_unterminated_name", `<x-foo`, []Token{
			{Kind: Text, Text: "<x-foo", Pos: at(1, 1)},
		}},
		{"xtag_unterminated_value", `<x-btn class="a`, []Token{
			{Kind: Text, Text: `<x-btn class="a`, Pos: at(1, 1)},
		}},
		// Empty component name -> the '<' is literal and scanning recovers.
		{"xtag_empty_name", `<x->`, []Token{
			{Kind: Text, Text: "<x->", Pos: at(1, 1)},
		}},
		{"xtag_bare", `<x-`, []Token{
			{Kind: Text, Text: "<x-", Pos: at(1, 1)},
		}},

		// Attributes on x-slot are lexed like any tag's, not special-cased.
		{"slot_with_attrs", `<x-slot:head class="p-4" :id="$id">`, []Token{
			{Kind: SlotOpen, Name: "head", Pos: at(1, 1), Attrs: []Attr{
				{Name: "class", Value: "p-4"},
				{Name: "id", Value: "$id", Bound: true},
			}},
		}},

		// A component whose name merely starts with "slot" is a normal close tag.
		{"slotfoo_close", `</x-slotfoo>`, []Token{
			{Kind: TagClose, Name: "slotfoo", Pos: at(1, 1)},
		}},

		// Unterminated openers recover so later constructs still scan.
		{"comment_unterminated_recovers", `{{-- x {{ $y }}`, []Token{
			{Kind: Text, Text: "{{-- x ", Pos: at(1, 1)},
			{Kind: Echo, Text: "$y", Pos: at(1, 8)},
		}},
		{"raw_unterminated", `{!! $x`, []Token{{Kind: Text, Text: "{!! $x", Pos: at(1, 1)}}},

		// @{{ unterminated -> raw from '@' onward, verbatim.
		{"escape_braces_unterminated", `@{{ raw`, []Token{
			{Kind: Text, Text: "@{{ raw", Pos: at(1, 1)},
		}},

		// Directive arg with a double-quoted string containing a ')'.
		{"directive_dquote_paren", `@class(["a" => ")"])`, []Token{
			{Kind: Directive, Name: "class", Text: `["a" => ")"]`, Pos: at(1, 1)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertTokens(t, Scan("t.blade.html", []byte(c.src)), c.want) })
	}
}

// TestScanWordBoundary pins the real-Blade \B@ rule: an '@' begins a directive
// or the @@ escape ONLY when the previous source byte is not a word char
// ([A-Za-z0-9_]); start-of-input counts as a boundary. The @{{ echo escape is
// exempt (real Blade's echo regex has no \B). Every expectation below was probed
// against real Blade via `pocketknife:render` (see task-14b report probe log).
func TestScanWordBoundary(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []Token
	}{
		// A '@' after a word char is not a directive: mailto addresses survive
		// verbatim (real Blade: `mailto:hello@example.com` unchanged).
		{"mailto", `mailto:hello@example.com`, []Token{
			{Kind: Text, Text: `mailto:hello@example.com`, Pos: at(1, 1)},
		}},
		{"email_directive_name", `user@if.test`, []Token{
			{Kind: Text, Text: `user@if.test`, Pos: at(1, 1)},
		}},
		// '@' after an underscore or digit (both word chars) stays literal.
		{"underscore_before_at", `_@if`, []Token{
			{Kind: Text, Text: `_@if`, Pos: at(1, 1)},
		}},
		{"digit_before_at", `1@if`, []Token{
			{Kind: Text, Text: `1@if`, Pos: at(1, 1)},
		}},
		// @@ after a word char is NOT the escape: the first '@' is literal text,
		// and the second '@' (now at a boundary) opens directive `b`. Real Blade
		// renders `a@@b` verbatim only because it leaves the unknown `@b`
		// directive literal — our app-tier Unknown handling is the separate,
		// documented Task 11 divergence and is out of scope here.
		{"at_at_after_word", `a@@b`, []Token{
			{Kind: Text, Text: `a@`, Pos: at(1, 1)},
			{Kind: Directive, Name: "b", Pos: at(1, 3)},
		}},
		// The @{{ echo escape ignores the boundary rule (real Blade drops the '@'
		// and prints the braces literally even after a word char).
		{"braces_escape_after_word", `hi@{{ x }}`, []Token{
			{Kind: Text, Text: `hi{{ x }}`, Pos: at(1, 1)},
		}},
		// A boundary '@' (after a space or punctuation) still opens a directive,
		// while the trailing @endif — pinned to the word char `y` — stays literal.
		{"directive_after_space", `x @if(true)y@endif`, []Token{
			{Kind: Text, Text: `x `, Pos: at(1, 1)},
			{Kind: Directive, Name: "if", Text: "true", Pos: at(1, 3)},
			{Kind: Text, Text: `y@endif`, Pos: at(1, 12)},
		}},
		{"directive_after_punct", `(@if(true)y@endif)`, []Token{
			{Kind: Text, Text: `(`, Pos: at(1, 1)},
			{Kind: Directive, Name: "if", Text: "true", Pos: at(1, 2)},
			{Kind: Text, Text: `y@endif)`, Pos: at(1, 11)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertTokens(t, Scan("t.blade.html", []byte(c.src)), c.want) })
	}
}
