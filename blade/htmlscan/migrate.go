package htmlscan

import (
	"errors"
	"strings"

	"github.com/thedevdojo/framework/blade/scanner"
)

// MigrateStats counts what Migrate changed in one file.
type MigrateStats struct {
	Values  int // {{ x }} now written {x}
	Escapes int // @{{ x }}, the old escape, now written \{\{ x }}
	Braces  int // braces that were text and would now open a value, now written \{
	Kept    int // {{ x }} left as it was, where {x} cannot say it
}

// Changed reports whether anything was rewritten.
func (m MigrateStats) Changed() bool { return m.Values+m.Escapes+m.Braces > 0 }

// Migrate rewrites an .html view written before single braces so that it
// reads today exactly as it read then. A value in double braces, where a
// single brace is read, becomes one: {{ post.title }} is {post.title}. The old
// escape @{{ x }} becomes \{\{ x }}. And a brace that was text but would now
// open a value — {title} in a code sample — is written \{title}, so it stays
// text. Nothing else in the file changes.
//
// The result is proved before it is returned: read today, it must give the
// tokens the original gave read the old way. An error says why it could not be
// rewritten; the file is then left as it was. Migrating a migrated file
// changes nothing.
func Migrate(file string, src []byte) ([]byte, MigrateStats, error) {
	s := &state{file: file, src: src, line: 1, col: 1, open: map[string][]string{}, legacy: true}
	var st MigrateStats
	w := &writer{st: &st}
	for !s.eof() {
		start := s.pos
		w.ctx = s.ctx // where the page stands before this step, today as then
		ntok := len(s.tokens)
		lastLen := -1
		if ntok > 0 && s.tokens[ntok-1].Kind == scanner.Text {
			lastLen = len(s.tokens[ntok-1].Text)
		}
		s.step()
		seg := string(src[start:s.pos])

		emitted, isText := "", false
		switch {
		case len(s.tokens) == ntok && lastLen >= 0 && len(s.tokens[ntok-1].Text) > lastLen:
			emitted, isText = s.tokens[ntok-1].Text[lastLen:], true
		case len(s.tokens) == ntok+1 && s.tokens[ntok].Kind == scanner.Text:
			emitted, isText = s.tokens[ntok].Text, true
		}

		switch {
		case isText && strings.HasPrefix(seg, "@{{") && emitted == seg[1:]:
			// The old escape: braces printed as they are.
			if w.reads() {
				w.escaped(emitted)
				st.Escapes++
			} else {
				w.verbatim(seg, emitted)
			}
		case isText:
			if emitted != seg {
				return src, MigrateStats{}, errors.New("cannot be rewritten: the scanner printed text that is not in the file")
			}
			w.text(seg)
		case strings.HasPrefix(seg, "{{--"):
			w.fence()
			w.construct(seg, false)
		case strings.HasPrefix(seg, "{!!"):
			w.fence()
			w.construct(seg, true)
		case strings.HasPrefix(seg, "{{"):
			inner := strings.TrimSpace(seg[2 : len(seg)-2])
			if w.reads() && SingleBrace(inner) && !strings.HasPrefix(inner, "!!") {
				w.fence()
				w.construct("{"+inner+"}", true)
				st.Values++
				continue
			}
			if w.reads() {
				st.Kept++
			}
			w.fence()
			w.construct(seg, true)
		case len(s.tokens) == ntok:
			w.out = append(w.out, seg...) // a closer swallowed with its refused opener
			w.tail = 0
		default:
			tag, err := migrateTag(s, start, seg, s.tokens[ntok:], &st)
			if err != nil {
				return src, MigrateStats{}, err
			}
			w.construct(tag, true)
		}
	}

	out := w.out
	got, info := ScanWith(file, out, Options{})
	if !sameTokens(s.tokens, got) {
		return src, MigrateStats{}, errors.New("cannot be rewritten exactly: read with single braces it would mean something else")
	}
	if len(info.DoubleBraces) > 0 {
		return src, MigrateStats{}, errors.New("cannot be rewritten: {{ }} would be left where {} would do")
	}
	return out, st, nil
}

// migrateTag rewrites the values of a component tag's text attributes, the
// one place inside a tag a brace is the template's.
func migrateTag(s *state, start int, seg string, toks []scanner.Token, st *MigrateStats) (string, error) {
	component, layout := false, false
	for _, t := range toks {
		if t.Kind == scanner.TagOpen {
			component = true
			lower := strings.ToLower(t.Tag)
			layout = lower == "layout" || lower == "include"
		}
	}
	if !component {
		return seg, nil
	}
	var b strings.Builder
	at := start
	srcSeen := false
	for _, sp := range s.spans {
		if layout && sp.name == "src" && !srcSeen {
			srcSeen = true
			continue
		}
		if !sp.hasValue || IsCodeAttr(sp.name) {
			continue
		}
		v := string(s.src[sp.start:sp.end])
		nv, err := migrateValue(v, st)
		if err != nil {
			return "", err
		}
		if nv == v {
			continue
		}
		b.Write(s.src[at:sp.start])
		b.WriteString(nv)
		at = sp.end
	}
	if at == start {
		return seg, nil
	}
	b.Write(s.src[at : start+len(seg)])
	return b.String(), nil
}

// migrateValue rewrites one component attribute's value the way Migrate
// rewrites text, keeping what it meant when only {{ }} and {!! !!} were read.
func migrateValue(v string, st *MigrateStats) (string, error) {
	if !strings.Contains(v, "{") {
		return v, nil
	}
	w := &writer{st: st, value: true}
	rest := v
	for {
		open, close := "{{", "}}"
		i := strings.Index(rest, "{{")
		if j := strings.Index(rest, "{!!"); j >= 0 && (i < 0 || j < i) {
			i, open, close = j, "{!!", "!!}"
		}
		end := -1
		if i >= 0 {
			end = strings.Index(rest[i+len(open):], close)
		}
		if i < 0 || end < 0 {
			w.text(rest)
			break
		}
		w.text(rest[:i])
		whole := rest[i : i+len(open)+end+len(close)]
		inner := strings.TrimSpace(rest[i+len(open) : i+len(open)+end])
		if err := w.fenceValue(); err != nil {
			return "", err
		}
		if open == "{{" && SingleBrace(inner) && !strings.HasPrefix(inner, "!!") {
			w.construct("{"+inner+"}", false)
			st.Values++
		} else {
			w.construct(whole, false)
			if open == "{{" {
				st.Kept++
			}
		}
		rest = rest[i+len(open)+end+len(close):]
	}
	return string(w.out), nil
}

// writer builds Migrate's output, following where the page stands.
type writer struct {
	out []byte
	// tail counts the '{' and '\' bytes the output ends in that are text: they
	// would fuse with a brace written next.
	tail  int
	ctx   Context
	value bool // inside a component attribute's value, where a brace is always read
	st    *MigrateStats
}

func (w *writer) reads() bool { return w.value || w.ctx.Reads() }

// put writes text the page prints as it is.
func (w *writer) put(s string) {
	w.out = append(w.out, s...)
	for i := 0; i < len(s); i++ {
		if !w.value {
			w.ctx.feed(s[i])
		}
		if s[i] == '{' || s[i] == '\\' {
			w.tail++
		} else {
			w.tail = 0
		}
	}
}

// brace writes a '{' that is text, as \{.
func (w *writer) brace() {
	w.out = append(w.out, '\\')
	w.put("{")
	w.tail = 0
}

// construct writes one of the template's own constructs.
func (w *writer) construct(s string, moves bool) {
	w.out = append(w.out, s...)
	w.tail = 0
	if moves && !w.value {
		w.ctx.Construct()
	}
}

// verbatim writes the source of a step that prints text, as it is.
func (w *writer) verbatim(src, printed string) {
	w.out = append(w.out, src...)
	if !w.value {
		w.ctx.Feed(printed)
	}
	w.tail = 0
}

// text writes text that held no construct when it was written. Where a brace
// is read, a brace that would now open a value, or that follows a backslash,
// is escaped; an opener that never closes ({{, {!! or {{-- with nothing to
// end it) stays as it is, text then as now.
func (w *writer) text(t string) {
	w.walk(t, false)
}

// escaped writes text that is the old escape's braces: they close, so they
// would open now.
func (w *writer) escaped(t string) {
	w.walk(t, true)
}

func (w *writer) walk(t string, closed bool) {
	for i := 0; i < len(t); {
		if t[i] != '{' || !w.reads() {
			w.put(t[i : i+1])
			i++
			continue
		}
		rest := t[i:]
		afterBackslash := w.tail > 0 && w.out[len(w.out)-1] == '\\'
		_, value := singleAt(rest)
		// An opener that closes later in the text would open now: the
		// braces the old escape printed do, and in an attribute's value
		// a {!! … !!} after a {{ left open was text then and is read now.
		opener, closes := "", false
		for _, o := range [][2]string{{"{{--", "--}}"}, {"{!!", "!!}"}, {"{{", "}}"}} {
			if strings.HasPrefix(rest, o[0]) {
				opener, closes = o[0], strings.Contains(rest[len(o[0]):], o[1])
				break
			}
		}
		switch {
		case afterBackslash || value || (opener != "" && (closed || closes)):
			w.brace()
			w.st.Braces++
			i++
		case opener != "":
			w.put(opener)
			i += len(opener)
		default:
			w.put("{")
			i++
		}
	}
}

// fence makes the text the output ends in safe before a construct that opens
// with a brace: a '{' is written \{, and a backslash, which would escape the
// brace after it, is printed through a hatch.
func (w *writer) fence() {
	if w.tail == 0 || !w.reads() {
		return
	}
	cut := len(w.out) - w.tail
	tail := string(w.out[cut:])
	w.out = w.out[:cut]
	for i := 0; i < len(tail); i++ {
		if tail[i] == '{' {
			w.out = append(w.out, '\\', '{')
		} else {
			w.out = append(w.out, Hatch(tail[i])...)
		}
	}
	w.tail = 0
}

// fenceValue is fence inside an attribute's value, where a hatch would be a
// value of its own: a backslash right before a value cannot be kept.
func (w *writer) fenceValue() error {
	if w.tail == 0 {
		return nil
	}
	cut := len(w.out) - w.tail
	tail := string(w.out[cut:])
	if strings.Contains(tail, `\`) {
		return errors.New(`cannot be rewritten: a component attribute has a backslash right before {{ }}; write it some other way first`)
	}
	w.out = w.out[:cut]
	for range tail {
		w.out = append(w.out, '\\', '{')
	}
	w.tail = 0
	return nil
}

// sameTokens reports whether two token streams say the same thing: positions
// aside, with a hatch read as the character it prints.
func sameTokens(a, b []scanner.Token) bool {
	x, y := level(a), level(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		p, q := x[i], y[i]
		if p.Kind != q.Kind || p.Text != q.Text || p.Name != q.Name || p.Tag != q.Tag || p.Src != q.Src ||
			p.SelfClose != q.SelfClose || len(p.Attrs) != len(q.Attrs) || strings.Join(p.Candidates, "|") != strings.Join(q.Candidates, "|") {
			return false
		}
		for j := range p.Attrs {
			m, n := p.Attrs[j], q.Attrs[j]
			if m.Name != n.Name || m.Value != n.Value || m.Bound != n.Bound || m.Valueless != n.Valueless {
				return false
			}
		}
	}
	return true
}

func level(toks []scanner.Token) []scanner.Token {
	var out []scanner.Token
	for _, t := range toks {
		if t.Kind == scanner.RawEcho && t.Text == `'\\'` {
			t = scanner.Token{Kind: scanner.Text, Text: `\`}
		}
		if n := len(out); t.Kind == scanner.Text && n > 0 && out[n-1].Kind == scanner.Text {
			out[n-1].Text += t.Text
			continue
		}
		out = append(out, t)
	}
	return out
}
