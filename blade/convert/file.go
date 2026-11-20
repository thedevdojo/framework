package convert

import (
	"bytes"
	"strings"

	"github.com/thedevdojo/framework/blade/htmlscan"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/blade/scanner"
)

// File converts one view's source into the syntax to, which must be the other
// one from the syntax path's extension says src is in. exists reports whether
// a component file exists, by its path relative to the components folder
// without an extension; it settles which file a tag like <SectionsHero> means.
//
// The converted source is returned with any problems. A file with problems
// must not be written: its output is there to be looked at, nothing more.
func File(path string, src []byte, to Syntax, exists func(base string) bool) (string, []render.Diagnostic) {
	if exists == nil {
		exists = func(string) bool { return false }
	}
	newPath := strings.TrimSuffix(path, to.other().Ext()) + to.Ext()

	var out string
	var problems []render.Diagnostic
	var bladeToks, htmlToks []scanner.Token
	if to == Studio {
		bladeToks = scanner.Scan(path, src)
		// The tidy print first: it is the one a person would have written.
		// It is kept only if it stands up to the same proof as any other.
		out, problems = printHTML(path, src, bladeToks, exists, true)
		htmlToks = htmlscan.Scan(newPath, []byte(out))
		if _, same := equivalent(bladeToks, htmlToks, true, exists); len(problems) > 0 || !same {
			out, problems = printHTML(path, src, bladeToks, exists, false)
			htmlToks = htmlscan.Scan(newPath, []byte(out))
		}
	} else {
		htmlToks = htmlscan.Scan(path, src)
		out, problems = printBlade(path, src, htmlToks, exists, true)
		bladeToks = scanner.Scan(newPath, []byte(out))
		if _, same := equivalent(bladeToks, htmlToks, false, exists); len(problems) > 0 || !same {
			out, problems = printBlade(path, src, htmlToks, exists, false)
			bladeToks = scanner.Scan(newPath, []byte(out))
		}
	}
	if len(problems) > 0 {
		return out, problems
	}

	// The proof: read the result back with the other scanner. If it does not
	// say what the original said, the construct at that spot has no exact
	// spelling in the other syntax (or the printer is wrong — either way the
	// file is not converted).
	if at, same := equivalent(bladeToks, htmlToks, to == Studio, exists); !same {
		origin := htmlToks
		if to == Studio {
			origin = bladeToks
		}
		pos := scanner.Pos{File: path, Line: 1, Col: 1}
		label := ""
		if at.ok {
			if tok, found := tokenAt(origin, at, to == Studio); found {
				pos, label = tok.Pos, tok.Label()
			}
		}
		target := to.Name()
		msg := "cannot be written in " + target + " exactly"
		if label != "" {
			msg = label + " " + msg
		}
		problems = append(problems, problem(path, pos.Line, pos.Col, msg))
	}
	return out, problems
}

// offsets maps a scanner position to its byte offset in src.
type offsets struct {
	src   []byte
	lines []int // lines[i] is the offset where line i+1 starts
}

func newOffsets(src []byte) offsets {
	lines := []int{0}
	for i, b := range src {
		if b == '\n' {
			lines = append(lines, i+1)
		}
	}
	return offsets{src: src, lines: lines}
}

// at returns the byte offset of p, clamped to the source.
func (o offsets) at(p scanner.Pos) int {
	if p.Line < 1 || p.Line > len(o.lines) {
		return len(o.src)
	}
	off := o.lines[p.Line-1] + p.Col - 1
	if off > len(o.src) {
		off = len(o.src)
	}
	return off
}

// indent returns the whitespace the line holding p starts with.
func (o offsets) indent(p scanner.Pos) string {
	if p.Line < 1 || p.Line > len(o.lines) {
		return ""
	}
	start := o.lines[p.Line-1]
	end := start
	for end < len(o.src) && (o.src[end] == ' ' || o.src[end] == '\t') {
		end++
	}
	return string(o.src[start:end])
}

// commentSource returns a {{-- … --}} comment exactly as written. A comment
// token carries no text, and both syntaxes spell a comment the same way.
func (o offsets) commentSource(p scanner.Pos) string {
	start := o.at(p)
	if !bytes.HasPrefix(o.src[start:], []byte("{{--")) {
		return "{{-- --}}"
	}
	end := bytes.Index(o.src[start+4:], []byte("--}}"))
	if end < 0 {
		return "{{-- --}}"
	}
	return string(o.src[start : start+4+end+4])
}

// hatch prints one character through a raw echo, so the scanner reading it
// sees an expression and not the start of whatever that character would have
// begun: {!! '<' !!}Button> prints <Button> and is not a tag.
func hatch(c byte) string { return "{!! '" + string(c) + "' !!}" }

// separator is a value that prints nothing. It stands between two things that
// would run together into something else (text@endif is not a directive).
const separator = "{{ '' }}"

// buffer is the output being written, with the two repairs both printers need
// at the seam between text and whatever comes next.
type buffer struct {
	b []byte
}

func (w *buffer) write(s string) { w.b = append(w.b, s...) }

func (w *buffer) last() byte {
	if len(w.b) == 0 {
		return 0
	}
	return w.b[len(w.b)-1]
}

// hatchLast reprints the last byte written through a hatch when it is one of
// set. Text that ends in '{' or '@' would otherwise fuse with a following
// "{{" into a different construct ("@{{" escapes; "{{{" starts early).
func (w *buffer) hatchLast(set string) {
	if n := len(w.b); n > 0 && strings.IndexByte(set, w.b[n-1]) >= 0 {
		c := w.b[n-1]
		w.b = append(w.b[:n-1], hatch(c)...)
	}
}

// hatchTrailing does the same for every such byte at the end, not only the
// last. Blade needs it: Laravel reads "@{!!" as an escape too, so a hatch must
// never be left standing after a plain '@'.
func (w *buffer) hatchTrailing(set string) {
	n := len(w.b)
	for n > 0 && strings.IndexByte(set, w.b[n-1]) >= 0 {
		n--
	}
	tail := string(w.b[n:])
	w.b = w.b[:n]
	for i := 0; i < len(tail); i++ {
		w.b = append(w.b, hatch(tail[i])...)
	}
}

func (w *buffer) String() string { return string(w.b) }

// pickQuote chooses the quote to wrap an attribute value in: the one the
// author used if the value allows it, else whichever the value does not
// contain. ok is false when it contains both, and no quote can hold it.
func pickQuote(value string, preferred byte) (q byte, ok bool) {
	if (preferred == '"' || preferred == '\'') && strings.IndexByte(value, preferred) < 0 {
		return preferred, true
	}
	switch {
	case !strings.Contains(value, `"`):
		return '"', true
	case !strings.Contains(value, `'`):
		return '\'', true
	}
	return 0, false
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool { return isIdentStart(b) || (b >= '0' && b <= '9') }

// isName reports whether s is a plain identifier.
func isName(s string) bool {
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

// isPath reports whether s is a dotted path of identifiers: post, post.author.name.
func isPath(s string) bool {
	for _, seg := range strings.Split(s, ".") {
		if !isName(seg) {
			return false
		}
	}
	return true
}

// phpString renders s as a single-quoted PHP string literal.
func phpString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}
