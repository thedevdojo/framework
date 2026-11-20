package htmlscan

// Context follows where a byte of an .html view stands as a browser reads it:
// in text, inside a tag, in an attribute's value, inside <script> or <style>,
// or inside an HTML comment. That decides whether a single brace there is the
// template's ({title}) or the page's own (a CSS rule, a JavaScript object, an
// Alpine expression).
//
// It is fed what the page will print — the text the scanner emits — and is
// told when a construct of the template's own stands in between (an echo, a
// tag). A construct counts as one ordinary byte: it can end a tag's name or
// start an attribute's value, and nothing else. Context is a plain value, so a
// copy of it is a snapshot.
type Context struct {
	st ctxState

	tag    [6]byte // the start of the tag name being read, lowercased
	tagLen int     // its full length (it may be longer than tag holds)
	endTag bool    // the tag being read is a closing tag

	attr    [3]byte // the start of the attribute name being read, lowercased
	attrLen int
	code    bool // the value being read is an attribute that holds code
	quote   byte

	raw    string // "script" or "style" while inside that element
	match  int    // how much of "</"+raw has been seen
	dashes int    // the run of '-' inside a comment

	// elem is the raw-text element as the scanner itself tracks it, which a
	// capitalized tag inside is code rather than a component. It is set at
	// the element's name rather than after its '>', exactly as it always was.
	elem string
}

type ctxState uint8

const (
	cData        ctxState = iota // text
	cLt                          // after '<'
	cEndLt                       // after "</"
	cTagName                     // reading a tag's name
	cInTag                       // between a tag's attributes
	cAttrName                    // reading an attribute's name
	cAfterName                   // after an attribute's name, before '=' or the next one
	cBeforeValue                 // after '='
	cQuoted                      // inside a quoted value
	cUnquoted                    // inside an unquoted value
	cBang                        // after "<!"
	cBangDash                    // after "<!-"
	cComment                     // inside <!-- -->
	cDecl                        // inside <!DOCTYPE …> and the like
	cRaw                         // inside <script> or <style>
	cRawEnd                      // inside one, right after "</script" or "</style"
)

// Reads reports whether a brace at this point is the template's.
func (c *Context) Reads() bool {
	switch c.st {
	case cRaw, cRawEnd, cComment, cBang, cBangDash, cDecl:
		return false
	case cBeforeValue, cQuoted, cUnquoted:
		return !c.code
	}
	return true
}

// Elem is the <script> or <style> the scanner is inside ("" for neither), as
// it decides whether a capitalized tag there is a component.
func (c *Context) Elem() string { return c.elem }

// Feed moves the context over text the page prints.
func (c *Context) Feed(text string) {
	for i := 0; i < len(text); i++ {
		c.feed(text[i])
	}
}

// Construct moves the context over a construct of the template's own.
func (c *Context) Construct() { c.feed(0) }

func (c *Context) feed(b byte) {
	for {
		switch c.st {
		case cData:
			if b == '<' {
				c.st = cLt
			}
			return
		case cLt:
			switch {
			case b == '/':
				c.st = cEndLt
				return
			case b == '!':
				c.st = cBang
				return
			case isLetter(b):
				c.startTag(b, false)
				return
			}
			c.st = cData
			continue
		case cEndLt:
			if isLetter(b) {
				c.startTag(b, true)
				return
			}
			c.st = cData
			continue
		case cTagName:
			switch {
			case isSpace(b) || b == '/':
				c.st = cInTag
			case b == '>':
				c.closeTag()
			default:
				if c.tagLen < len(c.tag) {
					c.tag[c.tagLen] = lowerByte(b)
				}
				c.tagLen++
			}
			return
		case cInTag:
			switch {
			case isSpace(b) || b == '/':
			case b == '>':
				c.closeTag()
			default:
				c.st = cAttrName
				c.attrLen = 0
				c.addAttr(b)
			}
			return
		case cAttrName:
			switch {
			case isSpace(b):
				c.st = cAfterName
			case b == '/':
				c.st = cInTag
			case b == '>':
				c.closeTag()
			case b == '=':
				c.startValue()
			default:
				c.addAttr(b)
			}
			return
		case cAfterName:
			switch {
			case isSpace(b):
			case b == '=':
				c.startValue()
			case b == '>':
				c.closeTag()
			case b == '/':
				c.st = cInTag
			default:
				c.st = cAttrName
				c.attrLen = 0
				c.addAttr(b)
			}
			return
		case cBeforeValue:
			switch {
			case isSpace(b):
			case b == '"' || b == '\'':
				c.quote = b
				c.st = cQuoted
			case b == '>':
				c.closeTag()
			default:
				c.st = cUnquoted
			}
			return
		case cQuoted:
			if b == c.quote {
				c.st, c.code = cInTag, false
			}
			return
		case cUnquoted:
			switch {
			case isSpace(b):
				c.st, c.code = cInTag, false
			case b == '>':
				c.closeTag()
			}
			return
		case cBang:
			if b == '-' {
				c.st = cBangDash
				return
			}
			c.st = cDecl
			continue
		case cBangDash:
			if b == '-' {
				c.st, c.dashes = cComment, 0
				return
			}
			c.st = cDecl
			continue
		case cComment:
			switch {
			case b == '-':
				c.dashes++
			case b == '>' && c.dashes >= 2:
				c.st = cData
			default:
				c.dashes = 0
			}
			return
		case cDecl:
			if b == '>' {
				c.st = cData
			}
			return
		case cRaw:
			want := "</" + c.raw
			if lowerByte(b) == want[c.match] {
				c.match++
				if c.match == len(want) {
					c.st = cRawEnd
				}
				return
			}
			c.match = 0
			if b == '<' {
				c.match = 1
			}
			return
		case cRawEnd:
			if isSpace(b) || b == '/' || b == '>' {
				// </script> — the element ends; the closing tag is read
				// like any other.
				c.raw, c.match = "", 0
				c.endTag = true
				c.tagLen = 0
				c.st = cInTag
				continue
			}
			c.st, c.match = cRaw, 0
			continue
		}
		return
	}
}

func (c *Context) startTag(b byte, end bool) {
	c.st = cTagName
	c.endTag = end
	c.tag[0] = lowerByte(b)
	c.tagLen = 1
}

func (c *Context) addAttr(b byte) {
	if c.attrLen < len(c.attr) {
		c.attr[c.attrLen] = lowerByte(b)
	}
	c.attrLen++
}

func (c *Context) startValue() {
	c.st = cBeforeValue
	c.code = isCodeAttr(string(c.attr[:min(c.attrLen, len(c.attr))]), c.attrLen)
}

// closeTag handles the '>' that ends a tag: after <script> or <style> the
// element's raw text begins.
func (c *Context) closeTag() {
	c.st, c.code = cData, false
	if c.endTag || c.raw != "" {
		return
	}
	switch name := string(c.tag[:min(c.tagLen, len(c.tag))]); {
	case c.tagLen == 6 && name == "script", c.tagLen == 5 && name == "style":
		c.raw, c.st, c.match = name, cRaw, 0
	}
}

// isCodeAttr reports whether an attribute holds code rather than text, from
// the first bytes of its lowercased name and its length: an on… handler,
// Alpine's x-… and Vue's v-…, and the :name and @name shorthands both use.
func isCodeAttr(prefix string, length int) bool {
	switch {
	case len(prefix) >= 2 && (prefix[:2] == "x-" || prefix[:2] == "v-"):
		return true
	case len(prefix) >= 2 && prefix[:2] == "on" && length > 2:
		return true
	case prefix != "" && (prefix[0] == ':' || prefix[0] == '@'):
		return true
	}
	return false
}

// IsCodeAttr reports whether an attribute named name holds code, so that a
// brace in its value is never the template's.
func IsCodeAttr(name string) bool {
	lower := make([]byte, 0, 3)
	for i := 0; i < len(name) && i < 3; i++ {
		lower = append(lower, lowerByte(name[i]))
	}
	return isCodeAttr(string(lower), len(name))
}

func isLetter(b byte) bool { return isLower(b) || isUpper(b) }

func lowerByte(b byte) byte {
	if isUpper(b) {
		return b + ('a' - 'A')
	}
	return b
}
