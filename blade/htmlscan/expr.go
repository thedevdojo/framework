package htmlscan

import "strings"

// Expr rewrites a friendly expression — the form an author writes inside
// {{ … }} or a tag attribute in an .html file — into the Blade-form expression
// the shared evaluator (blade/expr) understands. It is a token-level rewrite,
// never an evaluation, and it never fails: anything it does not recognize is
// copied through for the evaluator to diagnose.
//
//	post.title            →  $post->title
//	name = 'Tony'         →  $name == 'Tony'      (=, == and === all mean equals)
//	featured and not old  →  $featured && ! $old
//	posts[0].tags         →  $posts[0]->tags
//
// The rules:
//
//   - A bare word is a variable ($word) unless it is true/false/null, one of
//     the word operators and/or/not, a member name (it follows `.` or `->`), or
//     a function name (it is followed by `(`).
//   - A `.` glued between a name and a name is member access (`->`). Any other
//     `.` is left alone: it is the operator that joins text, to the evaluator
//     here as in Blade (`'Hi ' . name`).
//   - A lone `=` and `===` become `==`; `!==` becomes `!=`.
//   - String literals are copied verbatim — nothing inside quotes is rewritten.
//   - An expression already written in Blade form ($post->title) is unchanged,
//     so both spellings work in an .html file.
func Expr(src string) string {
	var b strings.Builder
	b.Grow(len(src) + 8)
	// member is true right after a member-access operator: the next word is a
	// property or method name, never a variable or keyword.
	member := false
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		case isSpace(c):
			b.WriteByte(c)
			i++
			continue // whitespace never ends a pending member access

		case c == '\'' || c == '"':
			j := stringEnd(src, i)
			b.WriteString(src[i:j])
			i = j

		case c == '$':
			// Blade-form variable: copy the sigil and its name through.
			j := i + 1
			for j < n && isIdentPart(src[j]) {
				j++
			}
			b.WriteString(src[i:j])
			i = j

		case isDigit(c):
			j := i
			for j < n && isDigit(src[j]) {
				j++
			}
			if j+1 < n && src[j] == '.' && isDigit(src[j+1]) {
				j++
				for j < n && isDigit(src[j]) {
					j++
				}
			}
			b.WriteString(src[i:j])
			i = j

		case isIdentStart(c):
			j := i
			for j < n && isIdentPart(src[j]) {
				j++
			}
			word := src[i:j]
			i = j
			if member {
				b.WriteString(word)
				break
			}
			switch strings.ToLower(word) {
			case "true", "false", "null":
				b.WriteString(word)
			case "and":
				b.WriteString("&&")
			case "or":
				b.WriteString("||")
			case "not":
				b.WriteString("!")
				if j < n && src[j] == '=' {
					// "not=" means nothing, but it must not become "!==",
					// which is an operator and would be read as one.
					b.WriteByte(' ')
				}
			default:
				if nextNonSpace(src, j) == '(' {
					b.WriteString(word) // a function call: the evaluator names it
				} else {
					b.WriteString("$" + word)
				}
			}

		case c == '.':
			if i > 0 && i+1 < n && endsOperand(src[i-1]) && isIdentStart(src[i+1]) {
				b.WriteString("->")
				i++
				member = true
				continue
			}
			b.WriteByte(c)
			i++

		case c == '-' && i+1 < n && src[i+1] == '>':
			b.WriteString("->")
			i += 2
			member = true
			continue

		case c == '=':
			// A run of '=' is one operator. One, two or three all mean
			// "equals"; anything longer is not an operator at all and is left
			// for the evaluator to refuse.
			j := i
			for j < n && src[j] == '=' {
				j++
			}
			switch run := j - i; {
			case run == 1 && j < n && src[j] == '>':
				b.WriteString("=>")
				j++
			case run <= 3:
				b.WriteString("==")
			default:
				b.WriteString(src[i:j])
			}
			i = j

		case c == '!':
			j := i + 1
			for j < n && src[j] == '=' {
				j++
			}
			switch run := j - i - 1; {
			case run == 0:
				b.WriteByte(c)
			case run <= 2:
				b.WriteString("!=")
			default:
				b.WriteString(src[i:j])
			}
			i = j

		case c == '<' || c == '>':
			// <=, >= and <=> keep their '=' — it is part of the operator, not
			// an equality sign of its own.
			j := i + 1
			if j < n && src[j] == '=' {
				j++
				if c == '<' && j < n && src[j] == '>' {
					j++
				}
			}
			b.WriteString(src[i:j])
			i = j

		default:
			b.WriteByte(c)
			i++
		}
		member = false
	}
	return b.String()
}

// stringEnd returns the index just past the string literal opening at src[i]
// (a quote). A backslash skips the byte after it; an unterminated literal runs
// to the end of src.
func stringEnd(src string, i int) int {
	quote := src[i]
	j := i + 1
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case quote:
			return j + 1
		}
		j++
	}
	return len(src)
}

// nextNonSpace returns the first non-whitespace byte at or after src[i], or 0.
func nextNonSpace(src string, i int) byte {
	for ; i < len(src); i++ {
		if !isSpace(src[i]) {
			return src[i]
		}
	}
	return 0
}

// endsOperand reports whether b can be the last byte of something a member
// access hangs off: a name, an index, or a parenthesized group.
func endsOperand(b byte) bool {
	return isIdentPart(b) || b == ']' || b == ')'
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

func isLower(b byte) bool { return b >= 'a' && b <= 'z' }

func isIdentStart(b byte) bool { return b == '_' || isLower(b) || isUpper(b) }

func isIdentPart(b byte) bool { return isIdentStart(b) || isDigit(b) }
