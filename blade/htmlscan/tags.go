package htmlscan

import (
	"strings"

	"github.com/thedevdojo/framework/blade/scanner"
)

// This file turns each built-in tag into the Blade token it is another
// spelling of:
//
//	<If condition="…"> … <ElseIf …> … <Else> … </If>   @if / @elseif / @else / @endif
//	<Foreach posts="post"> … </Foreach>                 @foreach ($posts as $post) / @endforeach
//	<Break /> <Continue />                              @break / @continue
//	<Layout src="main.html"> … </Layout>                <x-layouts.main> … </x-layouts.main>
//	<Include src="button.html" />                       <x-button />
//	<Slot name="title"> … </Slot>                       <x-slot:title> … </x-slot>
//	<Slot />  <Slot name="title" />                     {{ $slot }}  {{ $title ?? '' }}
//	<Props heading="Hello" />                           @props(['heading' => 'Hello'])
//	<Vite src="src/css/site.css" />                     @vite(['src/css/site.css'])

// openBuiltin emits the token(s) for an opening built-in tag. lower is the
// tag's lowercase name (the key into builtins); name is it as written.
func (s *state) openBuiltin(pos scanner.Pos, name, lower string, attrs []scanner.Attr, selfClose bool) {
	switch lower {
	case "if", "elseif":
		cond, ok := condition(attrs)
		if !ok {
			s.errorTok(pos, name, "<"+name+"> needs a condition, like <"+name+` condition="post.featured">`)
			cond = "false" // keep the block's shape so its <Else> and </If> still pair
		}
		s.emit(scanner.Token{Kind: scanner.Directive, Name: lower, Text: cond, Tag: name, Pos: pos})

	case "else":
		s.emit(scanner.Token{Kind: scanner.Directive, Name: "else", Tag: name, Pos: pos})

	case "break", "continue":
		cond, _ := condition(attrs) // no condition means "always"
		s.emit(scanner.Token{Kind: scanner.Directive, Name: lower, Text: cond, Tag: name, Pos: pos})

	case "foreach":
		text, limit, msg := foreachArgs(name, attrs)
		if msg != "" {
			s.refuse(pos, name, lower, msg, selfClose)
			return
		}
		tok := scanner.Token{Kind: scanner.Directive, Name: "foreach", Text: text, Tag: name, Pos: pos}
		if limit != nil {
			tok.Attrs = []scanner.Attr{*limit}
		}
		s.emit(tok)
		s.push(lower, lower)

	case "layout":
		s.openComponent(pos, name, lower, "layouts/", "layouts", attrs, selfClose)

	case "include":
		s.openComponent(pos, name, lower, "", "components", attrs, selfClose)

	case "slot":
		s.openSlot(pos, name, attrs, selfClose)

	case "props":
		s.emit(scanner.Token{Kind: scanner.Directive, Name: "props", Text: propsLiteral(attrs), Tag: name, Pos: pos})

	case "vite":
		s.emit(scanner.Token{Kind: scanner.Directive, Name: "vite", Text: viteLiteral(attrs), Tag: name, Pos: pos})
	}
}

// closeBuiltin emits the token for a closing built-in tag. The tags that never
// wrap content (<Else>, <Break />, <Props />, …) may be written with a closing
// tag for tidiness; it means nothing and emits nothing.
func (s *state) closeBuiltin(pos scanner.Pos, name, lower string) {
	switch lower {
	case "if":
		s.emit(scanner.Token{Kind: scanner.Directive, Name: "endif", Tag: name, Pos: pos})

	case "foreach":
		if val, ok := s.pop(lower); ok && val == "" {
			return // its opener was refused; the error is already reported
		}
		s.emit(scanner.Token{Kind: scanner.Directive, Name: "endforeach", Tag: name, Pos: pos})

	case "layout", "include":
		val, ok := s.pop(lower)
		if ok && val == "" {
			return
		}
		// A closer with no opener keeps an empty Name: nothing can match it, so
		// the parser reports it as stray.
		s.emit(scanner.Token{Kind: scanner.TagClose, Name: val, Tag: name, Pos: pos})

	case "slot":
		if val, ok := s.pop(lower); ok && val == "" {
			return
		}
		s.emit(scanner.Token{Kind: scanner.SlotClose, Tag: name, Pos: pos})
	}
}

// refuse reports a tag that is missing something it needs. The Error token
// carries the message; a paired tag's closer is marked to be swallowed, so the
// content between them renders as if the tag were not there and the mistake is
// reported exactly once.
func (s *state) refuse(pos scanner.Pos, name, lower, msg string, selfClose bool) {
	s.errorTok(pos, name, msg)
	if !selfClose {
		s.push(lower, "")
	}
}

// openComponent handles <Layout src> and <Include src>: both render the
// component file src names, found under dir inside the components folder.
// Every attribute but src is passed to that component.
func (s *state) openComponent(pos scanner.Pos, name, lower, dir, folder string, attrs []scanner.Attr, selfClose bool) {
	var src *scanner.Attr
	rest := make([]scanner.Attr, 0, len(attrs))
	for i := range attrs {
		if attrs[i].Name == "src" && src == nil {
			src = &attrs[i]
			continue
		}
		rest = append(rest, attrs[i])
	}
	example := "<" + name + ` src="` + map[string]string{"layout": "main", "include": "button"}[lower] + `.html">`
	switch {
	case src == nil || src.Valueless:
		s.refuse(pos, name, lower, "<"+name+"> needs a src, like "+example, selfClose)
		return
	case src.Bound:
		s.refuse(pos, name, lower, "<"+name+"> src must be a plain file name, like "+example, selfClose)
		return
	}
	base, ok := srcBase(dir, src.Value)
	if !ok {
		if strings.TrimSpace(src.Value) == "" {
			s.refuse(pos, name, lower, "<"+name+"> needs a src, like "+example, selfClose)
		} else {
			s.refuse(pos, name, lower, "<"+name+`> src "`+src.Value+`" is not a file inside the `+folder+" folder", selfClose)
		}
		return
	}
	s.emit(scanner.Token{
		Kind: scanner.TagOpen, Name: dotted(base), Tag: name, Candidates: []string{base},
		Attrs: s.componentAttrs(rest), SelfClose: selfClose, Pos: pos,
	})
	if !selfClose {
		s.push(lower, dotted(base))
	}
}

// openSlot handles <Slot>. A pair fills a slot of the component it sits in
// (<Slot name="title">…</Slot>); a self-closing tag prints one (<Slot /> the
// default slot, <Slot name="title" /> a named one — nothing when the caller
// did not fill it).
func (s *state) openSlot(pos scanner.Pos, name string, attrs []scanner.Attr, selfClose bool) {
	slot := ""
	for _, a := range attrs {
		if a.Name == "name" && !a.Valueless && !a.Bound {
			slot = strings.TrimSpace(a.Value)
		}
	}
	if slot != "" && !isSlotName(slot) {
		s.refuse(pos, name, "slot", "<"+name+`> name "`+slot+`" must be letters, numbers, dashes or underscores`, selfClose)
		return
	}
	if selfClose {
		if slot == "" {
			s.emit(scanner.Token{Kind: scanner.Echo, Text: "$slot", Tag: name, Pos: pos})
		} else {
			s.emit(scanner.Token{Kind: scanner.Echo, Text: "$" + bindingName(slot) + " ?? ''", Tag: name, Pos: pos})
		}
		return
	}
	if slot == "" {
		s.refuse(pos, name, "slot", "<"+name+"> needs a name to fill a slot, like <"+name+` name="title">; <`+name+" /> prints the default slot", false)
		return
	}
	s.emit(scanner.Token{Kind: scanner.SlotOpen, Name: slot, Tag: name, Pos: pos})
	s.push("slot", slot)
}

// condition builds the Blade condition for <If>, <ElseIf>, <Break> and
// <Continue> from the tag's attributes. ok is false when there is none.
//
//	condition="post.views > 100"   any expression
//	not="post.draft"               the expression, negated
//	featured                       shorthand: the value is truthy
//	status="published"             shorthand: the value equals this text
//	:plan="site.plan"              shorthand: the value equals this expression
//
// Several attributes must all hold.
func condition(attrs []scanner.Attr) (string, bool) {
	var parts []string
	for _, a := range attrs {
		switch {
		case a.Valueless:
			parts = append(parts, Expr(a.Name))
		case a.Name == "condition":
			if strings.TrimSpace(a.Value) == "" {
				continue
			}
			parts = append(parts, Expr(a.Value))
		case a.Name == "not":
			if strings.TrimSpace(a.Value) == "" {
				continue
			}
			parts = append(parts, "!("+Expr(a.Value)+")")
		case a.Bound:
			parts = append(parts, Expr(a.Name)+" == ("+Expr(a.Value)+")")
		default:
			parts = append(parts, Expr(a.Name)+" == "+literal(a.Value))
		}
	}
	switch len(parts) {
	case 0:
		return "", false
	case 1:
		return parts[0], true
	}
	for i, p := range parts {
		parts[i] = "(" + p + ")"
	}
	return strings.Join(parts, " && "), true
}

// foreachArgs builds the Blade @foreach arguments — "$posts as $post" — from a
// <Foreach> tag's attributes, plus its optional limit. msg is non-empty when
// the tag cannot be read.
//
//	posts="post"                    the list, and what to call each item
//	posts="key, post"               … with the key too
//	collection="posts" as="post"    the long form (DevDojo Static's), for a
//	                                list that is not a plain name: posts[0].tags
//	limit="3"                       only the first three (count is an alias)
func foreachArgs(name string, attrs []scanner.Attr) (text string, limit *scanner.Attr, msg string) {
	var coll, as, key string
	hasColl := false
	var short *scanner.Attr
	for i := range attrs {
		a := &attrs[i]
		switch a.Name {
		case "collection":
			coll, hasColl = strings.TrimSpace(a.Value), true
		case "as":
			as = a.Value
		case "key":
			key = a.Value
		case "limit", "count":
			limit = &scanner.Attr{Name: "limit", Value: Expr(strings.TrimSpace(a.Value)), Pos: a.Pos}
		default:
			if short != nil {
				return "", nil, "<" + name + "> loops over one list at a time"
			}
			short = a
		}
	}
	needsBoth := "<" + name + "> needs a list and a name for each item, like <" + name + ` posts="post">`
	switch {
	case short != nil && hasColl:
		return "", nil, "<" + name + "> loops over one list at a time"
	case short != nil:
		if short.Valueless || strings.TrimSpace(short.Value) == "" {
			return "", nil, needsBoth
		}
		coll, as = short.Name, short.Value
	case hasColl:
		if strings.TrimSpace(as) == "" {
			// DevDojo Static names each item after the collection itself.
			if !isPlainName(coll) {
				return "", nil, "<" + name + `> needs a name for each item, like as="item"`
			}
			as = coll
		}
	default:
		return "", nil, needsBoth
	}

	k, v := splitKeyValue(as)
	if strings.TrimSpace(key) != "" {
		k = loopVar(key)
	}
	text = Expr(coll) + " as "
	if k != "" {
		text += "$" + k + " => "
	}
	return text + "$" + v, limit, ""
}

// splitKeyValue reads what a loop calls each item: "post", or a key and a value
// as "key, post" or "key => post".
func splitKeyValue(as string) (key, val string) {
	sep, width := strings.Index(as, "=>"), 2
	if sep < 0 {
		sep, width = strings.Index(as, ","), 1
	}
	if sep < 0 {
		return "", loopVar(as)
	}
	return loopVar(as[:sep]), loopVar(as[sep+width:])
}

// loopVar trims a loop variable name and drops a leading '$', so both post and
// $post name the same variable.
func loopVar(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "$")
}

// propsLiteral builds the @props array literal from a <Props> tag: a plain
// attribute is a text default (see propText), a :bound attribute's value is taken as the
// literal it is (:count="3", :open="false", :items="[]"), and a bare attribute
// names a prop that is simply empty (null) until a tag passes it.
//
// That last one is deliberately not Blade's bare @props(['badge']), which
// makes the prop REQUIRED — leave it off a tag and the file hits an undefined
// variable. An HTML author listing <Props title badge /> is naming what the
// component accepts, as in every other component system; optional is what
// they mean.
func propsLiteral(attrs []scanner.Attr) string {
	parts := make([]string, 0, len(attrs))
	for _, a := range attrs {
		name := phpString(bindingName(a.Name))
		switch {
		case a.Valueless:
			parts = append(parts, name+" => null")
		case a.Bound:
			parts = append(parts, name+" => "+strings.TrimSpace(a.Value))
		default:
			parts = append(parts, name+" => "+phpString(propText.Replace(a.Value)))
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// propText reads a <Props> text default the way HTML reads an attribute, for
// the four characters that matter there: &quot; is a double quote, &#39; and
// &apos; an apostrophe, &amp; an ampersand. It is what lets a default hold
// both kinds of quotes (body="can't say &quot;better&quot;"); Blade's @props,
// written in parentheses rather than in an attribute, never had the problem.
// Nothing else is decoded — &lt; and &copy; mean themselves, as text does
// everywhere else in a template — and an ampersand on its own is left alone.
var propText = strings.NewReplacer("&quot;", `"`, "&#39;", "'", "&apos;", "'", "&amp;", "&")

// QuoteProp writes text as the value of a <Props> text default, quotes
// included, such that the scanner reads exactly text back. It uses whichever
// quote the text does not contain, and &quot; when it contains both.
func QuoteProp(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		// An ampersand that would be read as one of the four references is a
		// literal one: spell it &amp; so it stays an ampersand.
		if text[i] == '&' {
			rest := text[i:]
			if strings.HasPrefix(rest, "&quot;") || strings.HasPrefix(rest, "&#39;") ||
				strings.HasPrefix(rest, "&apos;") || strings.HasPrefix(rest, "&amp;") {
				b.WriteString("&amp;")
				continue
			}
		}
		b.WriteByte(text[i])
	}
	s := b.String()
	switch {
	case !strings.Contains(s, `"`):
		return `"` + s + `"`
	case !strings.Contains(s, "'"):
		return "'" + s + "'"
	}
	return `"` + strings.ReplaceAll(s, `"`, "&quot;") + `"`
}

// viteLiteral builds the @vite argument from <Vite src="a.css, b.css" />.
func viteLiteral(attrs []scanner.Attr) string {
	var entries []string
	for _, a := range attrs {
		if a.Name != "src" || a.Valueless {
			continue
		}
		for _, e := range strings.Split(a.Value, ",") {
			if e = strings.TrimSpace(e); e != "" {
				entries = append(entries, phpString(e))
			}
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return "[" + strings.Join(entries, ", ") + "]"
}

// literal renders a shorthand attribute value as the Blade literal it means:
// true, false, null and plain numbers keep their type, anything else is text.
func literal(v string) string {
	t := strings.TrimSpace(v)
	switch strings.ToLower(t) {
	case "true", "false", "null":
		return strings.ToLower(t)
	}
	if isNumber(t) {
		return t
	}
	return phpString(v)
}

// phpString renders s as a single-quoted PHP string literal. Backslash and the
// quote are the only two characters such a literal escapes.
func phpString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// isNumber reports whether s is a plain integer or decimal, optionally negative.
func isNumber(s string) bool {
	s = strings.TrimPrefix(s, "-")
	digits, dots := 0, 0
	for i := 0; i < len(s); i++ {
		switch {
		case isDigit(s[i]):
			digits++
		case s[i] == '.' && i > 0 && i < len(s)-1:
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

// isPlainName reports whether s is a single variable name (posts, not
// posts[0].tags).
func isPlainName(s string) bool {
	s = strings.TrimPrefix(s, "$")
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}

// isSlotName reports whether s can name a slot: letters, digits, dashes and
// underscores, starting with a letter or underscore.
func isSlotName(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) && s[i] != '-' {
			return false
		}
	}
	return true
}

// bindingName maps a dashed name to the variable it binds, the way Blade's
// Str::camel does for slot names and prop attributes: logo-text → logoText. A
// name with no dash is already a variable name and is left alone (logo_text
// stays logo_text).
func bindingName(name string) string {
	if !strings.Contains(name, "-") {
		return name
	}
	var b strings.Builder
	b.Grow(len(name))
	upperNext, first := false, true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' || c == '_' {
			upperNext = !first
			continue
		}
		switch {
		case first:
			if isUpper(c) {
				c += 'a' - 'A'
			}
			first = false
		case upperNext:
			if isLower(c) {
				c -= 'a' - 'A'
			}
			upperNext = false
		}
		b.WriteByte(c)
	}
	return b.String()
}
