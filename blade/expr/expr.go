// Package expr evaluates the small expression language that lives inside
// {{ … }} echoes and @if/@elseif/@foreach conditions. It is the safe heart of
// the renderer: it never calls a PHP interpreter, never uses eval/reflection,
// and never panics on user input — malformed input becomes a typed error.
//
// The grammar is a deliberate subset of PHP expressions:
//
//	primary   := $name | int | float | 'str' | "str" | true | false | null | '(' expr ')'
//	postfix   := primary ( '->' name | '[' expr ']' )*
//	unary     := ('!' | '-') unary | postfix
//	compare   := unary ( ('==' '!=' '<' '>' '<=' '>=') unary )?   // non-associative
//	and       := compare ( '&&' compare )*
//	or        := and ( '||' and )*
//	coalesce  := or ( '??' coalesce )?                            // right-assoc, lowest
//
// Anything valid in PHP but outside this subset (arithmetic, concatenation,
// function/method calls, ?->, ::, assignments, …) is reported as a
// *SyntaxError with AppTier=true, signalling the renderer to fall back to the
// real Blade/PHP "app tier". Pure garbage is a *SyntaxError with AppTier=false.
//
// Value semantics track PHP 8: loose equality, truthiness, numeric-string
// comparison, and null-propagating property/index access (a missing key or a
// property on null yields nil, which the `??` operator is designed to guard).
// Only the Go standard library is imported.
package expr

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Value is a dynamically typed template value. The closed set of concrete
// types is: nil, bool, int64, float64, string, []Value, map[string]Value,
// *Object. *Object carries PHP-stdClass semantics (order-preserving, always
// truthy, `->` reads members, `[]` is an error); map[string]Value keeps PHP
// associative-array semantics. JSON objects decode to *Object (see object.go),
// so every stdClass the SiteData contract binds mirrors real Blade.
type Value any

// Scope resolves a variable name (WITHOUT the leading '$') to its value. The
// bool reports whether the variable is defined; a defined variable may hold a
// nil Value.
type Scope interface {
	Lookup(name string) (Value, bool)
}

// ErrUndefinedVar is the sentinel wrapped (with the variable name) when Eval
// reaches a variable the scope does not define and it is not protected by `??`.
var ErrUndefinedVar = errors.New("undefined variable")

// UndefinedVarError is the concrete error Eval returns for an undefined
// variable. It wraps ErrUndefinedVar (Unwrap returns the sentinel, so
// errors.Is(err, ErrUndefinedVar) keeps working for every existing caller)
// while also exposing the variable's Name in structured form for callers that
// need to special-case a particular name.
type UndefinedVarError struct {
	Name string
}

func (e *UndefinedVarError) Error() string {
	return fmt.Sprintf("undefined variable $%s", e.Name)
}

func (e *UndefinedVarError) Unwrap() error { return ErrUndefinedVar }

// ErrUndefinedProp is the sentinel wrapped when `->member` reads a property an
// *Object does not have. It mirrors real Blade's "Undefined property:
// stdClass::$x" (an ErrorException that aborts the render) — but, like
// ErrUndefinedVar, it is SUPPRESSED by `??`/isset, so `$obj->missing ?? 'd'`
// still yields 'd' (oracle-probed). The renderer maps it to
// error/undefined-property.
var ErrUndefinedProp = errors.New("undefined property")

// UndefinedPropError is the concrete error for a missing *Object member; it
// wraps ErrUndefinedProp and exposes the property Name, mirroring
// UndefinedVarError.
type UndefinedPropError struct {
	Name string
}

func (e *UndefinedPropError) Error() string {
	return fmt.Sprintf("undefined property $%s", e.Name)
}

func (e *UndefinedPropError) Unwrap() error { return ErrUndefinedProp }

// ErrObjectAsArray is returned when `[index]` is applied to an *Object. It
// mirrors PHP's fatal "Cannot use object of type stdClass as array" — and,
// unlike a missing member, `??`/isset does NOT suppress it (isset on
// object-as-array itself throws; oracle-probed `$obj['k'] ?? 'd'` still fatals).
// So it must be a plain error that propagates through a coalesce. The renderer
// maps it to error/object-as-array.
var ErrObjectAsArray = errors.New("cannot use object of type stdClass as array")

// errArrayOrder is returned when an ordered comparison (< > <= >=) involves an
// array or map — PHP's ordered array comparison is out of scope for v1. It is a
// plain error: not ErrUndefinedVar and not a *SyntaxError, so @if treats it as
// false while the renderer can still tell it apart.
var errArrayOrder = errors.New("cannot order-compare arrays")

// errObjectOrder mirrors errArrayOrder for objects: an ordered comparison
// (< > <= >=) with an *Object on either side is out of v1's subset (real Blade
// compares stdClass property-wise, or fatals against a number). @if treats it as
// false; the renderer reports error/expression.
var errObjectOrder = errors.New("cannot order-compare objects")

// errObjectNumber is returned when `== / !=` compares an *Object with a number.
// Real Blade fatals ("Object of class stdClass could not be converted to
// int/float"), so the engine surfaces an error rather than silently succeeding.
var errObjectNumber = errors.New("cannot compare object with a number")

// SyntaxError describes input Eval could not evaluate. AppTier distinguishes
// valid-Blade-needing-PHP (true) from malformed input (false).
type SyntaxError struct {
	Msg     string
	AppTier bool
}

func (e *SyntaxError) Error() string { return e.Msg }

// Eval parses and evaluates src against scope.
func Eval(src string, scope Scope) (Value, error) {
	node, err := parse(src)
	if err != nil {
		return nil, err
	}
	return evalNode(node, scope)
}

// RequiredVars returns, sorted and de-duplicated, the names (without '$') of
// the variables whose undefined-ness would make Eval fail — i.e. every
// referenced variable except those protected by a `??` on their left. A parse
// failure returns the same error Eval would.
func RequiredVars(src string) ([]string, error) {
	node, err := parse(src)
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	collectVars(node, set)
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// collectVars walks the tree gathering required variable names. The LEFT
// operand of a `??` is protected (Eval catches its ErrUndefinedVar), so only
// the fallback subtree contributes.
func collectVars(node exprNode, set map[string]struct{}) {
	switch n := node.(type) {
	case *varNode:
		set[n.name] = struct{}{}
	case *litNode:
		// no variables
	case *propNode:
		collectVars(n.recv, set)
	case *indexNode:
		collectVars(n.recv, set)
		collectVars(n.index, set)
	case *unaryNode:
		collectVars(n.operand, set)
	case *compareNode:
		collectVars(n.left, set)
		collectVars(n.right, set)
	case *logicalNode:
		collectVars(n.left, set)
		collectVars(n.right, set)
	case *coalesceNode:
		collectVars(n.right, set)
	case *arrayNode:
		for _, e := range n.entries {
			if e.key != nil {
				collectVars(e.key, set)
			}
			collectVars(e.val, set)
		}
	case *callNode:
		// An undefined receiver or argument still fails Eval (the receiver
		// failure surfaces as the app-tier method-call error), so both sides
		// count as required.
		collectVars(n.recv, set)
		for _, a := range n.args {
			collectVars(a, set)
		}
	}
}

// ---------------------------------------------------------------------------
// AST
// ---------------------------------------------------------------------------

type exprNode interface{}

type varNode struct{ name string }

type litNode struct{ val Value }

type propNode struct {
	recv exprNode
	name string
}

type indexNode struct {
	recv  exprNode
	index exprNode
}

type unaryNode struct {
	op      string // "!" or "-"
	operand exprNode
}

type compareNode struct {
	op          string // == != < > <= >=
	left, right exprNode
}

type logicalNode struct {
	op          string // && or ||
	left, right exprNode
}

type coalesceNode struct {
	left, right exprNode
}

// arrayNode is an array literal: ['a', 'k' => $v, …]. An entry without a key
// takes the next auto index at evaluation time.
type arrayNode struct {
	entries []arrayEntryNode
}

type arrayEntryNode struct {
	key exprNode // nil for a keyless entry
	val exprNode
}

// callNode is a method call: recv->name(args…). Only the $attributes bag's
// modeled subset evaluates; every other receiver/method reports app-tier.
type callNode struct {
	recv exprNode
	name string
	args []exprNode
}

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tVar
	tInt
	tFloat
	tStr
	tTrue
	tFalse
	tNull
	tIdent
	tArrow
	tLBracket
	tRBracket
	tLParen
	tRParen
	tCoalesce
	tEq
	tNe
	tLt
	tGt
	tLe
	tGe
	tAnd
	tOr
	tNot
	tMinus
	tComma
	tDoubleArrow // => (array key separator)
	tAppTier     // an operator/construct that is valid PHP but outside v1's subset
)

type token struct {
	kind tokKind
	text string // identifier/var name, string value, or app-tier description
	num  Value  // int64 (tInt) or float64 (tFloat)
	pos  int
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

func tokenize(src string) ([]token, error) {
	var toks []token
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		case isSpace(c):
			i++
		case c == '$':
			if i+1 < n && (src[i+1] == '$' || src[i+1] == '{') {
				toks = append(toks, token{kind: tAppTier, text: "variable variables ($$)", pos: i})
				i += 2
				continue
			}
			if i+1 < n && isIdentStart(src[i+1]) {
				j := i + 1
				for j < n && isIdentPart(src[j]) {
					j++
				}
				toks = append(toks, token{kind: tVar, text: src[i+1 : j], pos: i})
				i = j
				continue
			}
			return nil, &SyntaxError{Msg: "unexpected '$'", AppTier: false}
		case c == '\'':
			s, ni, err := scanString(src, i, '\'')
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tStr, text: s, pos: i})
			i = ni
		case c == '"':
			s, ni, err := scanString(src, i, '"')
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tStr, text: s, pos: i})
			i = ni
		case c >= '0' && c <= '9':
			tk, ni, err := scanNumber(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tk)
			i = ni
		case isIdentStart(c):
			j := i
			for j < n && isIdentPart(src[j]) {
				j++
			}
			word := src[i:j]
			switch strings.ToLower(word) {
			case "true":
				toks = append(toks, token{kind: tTrue, pos: i})
			case "false":
				toks = append(toks, token{kind: tFalse, pos: i})
			case "null":
				toks = append(toks, token{kind: tNull, pos: i})
			default:
				toks = append(toks, token{kind: tIdent, text: word, pos: i})
			}
			i = j
		case c == '-':
			if i+1 < n && src[i+1] == '>' {
				toks = append(toks, token{kind: tArrow, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tMinus, pos: i})
				i++
			}
		case c == '[':
			toks = append(toks, token{kind: tLBracket, pos: i})
			i++
		case c == ']':
			toks = append(toks, token{kind: tRBracket, pos: i})
			i++
		case c == '(':
			toks = append(toks, token{kind: tLParen, pos: i})
			i++
		case c == ')':
			toks = append(toks, token{kind: tRParen, pos: i})
			i++
		case c == '?':
			switch {
			case i+1 < n && src[i+1] == '?':
				toks = append(toks, token{kind: tCoalesce, pos: i})
				i += 2
			case i+2 < n && src[i+1] == '-' && src[i+2] == '>':
				toks = append(toks, token{kind: tAppTier, text: "nullsafe operator (?->)", pos: i})
				i += 3
			default:
				toks = append(toks, token{kind: tAppTier, text: "ternary operator (?)", pos: i})
				i++
			}
		case c == '=':
			switch {
			case i+2 < n && src[i+1] == '=' && src[i+2] == '=':
				toks = append(toks, token{kind: tAppTier, text: "identity operator (===)", pos: i})
				i += 3
			case i+1 < n && src[i+1] == '=':
				toks = append(toks, token{kind: tEq, pos: i})
				i += 2
			case i+1 < n && src[i+1] == '>':
				toks = append(toks, token{kind: tDoubleArrow, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tAppTier, text: "assignment (=)", pos: i})
				i++
			}
		case c == '!':
			switch {
			case i+2 < n && src[i+1] == '=' && src[i+2] == '=':
				toks = append(toks, token{kind: tAppTier, text: "non-identity (!==)", pos: i})
				i += 3
			case i+1 < n && src[i+1] == '=':
				toks = append(toks, token{kind: tNe, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tNot, pos: i})
				i++
			}
		case c == '<':
			switch {
			case i+2 < n && src[i+1] == '=' && src[i+2] == '>':
				toks = append(toks, token{kind: tAppTier, text: "spaceship (<=>)", pos: i})
				i += 3
			case i+1 < n && src[i+1] == '=':
				toks = append(toks, token{kind: tLe, pos: i})
				i += 2
			case i+1 < n && src[i+1] == '<':
				toks = append(toks, token{kind: tAppTier, text: "shift (<<)", pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tLt, pos: i})
				i++
			}
		case c == '>':
			switch {
			case i+1 < n && src[i+1] == '=':
				toks = append(toks, token{kind: tGe, pos: i})
				i += 2
			case i+1 < n && src[i+1] == '>':
				toks = append(toks, token{kind: tAppTier, text: "shift (>>)", pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tGt, pos: i})
				i++
			}
		case c == '&':
			if i+1 < n && src[i+1] == '&' {
				toks = append(toks, token{kind: tAnd, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tAppTier, text: "bitwise and (&)", pos: i})
				i++
			}
		case c == '|':
			if i+1 < n && src[i+1] == '|' {
				toks = append(toks, token{kind: tOr, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tAppTier, text: "bitwise or (|)", pos: i})
				i++
			}
		case c == '+':
			if i+1 < n && src[i+1] == '=' {
				toks = append(toks, token{kind: tAppTier, text: "assignment (+=)", pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tAppTier, text: "arithmetic (+)", pos: i})
				i++
			}
		case c == '*':
			toks = append(toks, token{kind: tAppTier, text: "arithmetic (*)", pos: i})
			i++
		case c == '/':
			toks = append(toks, token{kind: tAppTier, text: "arithmetic (/)", pos: i})
			i++
		case c == '%':
			toks = append(toks, token{kind: tAppTier, text: "arithmetic (%)", pos: i})
			i++
		case c == '.':
			// A leading digit is consumed by scanNumber, so a bare '.' here is
			// always the concatenation operator (app tier).
			toks = append(toks, token{kind: tAppTier, text: "concatenation (.)", pos: i})
			i++
		case c == ',':
			toks = append(toks, token{kind: tComma, pos: i})
			i++
		case c == ':':
			if i+1 < n && src[i+1] == ':' {
				toks = append(toks, token{kind: tAppTier, text: "static access (::)", pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tAppTier, text: "colon (:)", pos: i})
				i++
			}
		default:
			return nil, &SyntaxError{Msg: fmt.Sprintf("unexpected character %q", string(rune(c))), AppTier: false}
		}
	}
	toks = append(toks, token{kind: tEOF, pos: i})
	return toks, nil
}

// scanString reads a quoted literal starting at src[start] (== quote). Only
// \\ and the matching quote are recognized escapes; any other backslash is
// kept literally, and there is no interpolation.
func scanString(src string, start int, quote byte) (string, int, error) {
	var b strings.Builder
	i, n := start+1, len(src)
	for i < n {
		c := src[i]
		if c == '\\' && i+1 < n {
			if nx := src[i+1]; nx == quote || nx == '\\' {
				b.WriteByte(nx)
				i += 2
				continue
			}
			b.WriteByte('\\')
			i++
			continue
		}
		if c == quote {
			return b.String(), i + 1, nil
		}
		b.WriteByte(c)
		i++
	}
	return "", 0, &SyntaxError{Msg: "unterminated string literal", AppTier: false}
}

// scanNumber reads an int or float matching \d+(\.\d+)? (no leading-dot, no
// exponent — those fall outside the literal grammar).
func scanNumber(src string, start int) (token, int, error) {
	i, n := start, len(src)
	for i < n && src[i] >= '0' && src[i] <= '9' {
		i++
	}
	isFloat := false
	if i < n && src[i] == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9' {
		isFloat = true
		i++
		for i < n && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	}
	text := src[start:i]
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return token{}, 0, &SyntaxError{Msg: "invalid number literal " + text, AppTier: false}
		}
		return token{kind: tFloat, num: f, pos: start}, i, nil
	}
	if v, err := strconv.ParseInt(text, 10, 64); err == nil {
		return token{kind: tInt, num: v, pos: start}, i, nil
	}
	// Integer overflow: fall back to a float value.
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return token{}, 0, &SyntaxError{Msg: "invalid number literal " + text, AppTier: false}
	}
	return token{kind: tFloat, num: f, pos: start}, i, nil
}

// ---------------------------------------------------------------------------
// Parser (recursive descent, precedence-climbing)
// ---------------------------------------------------------------------------

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }

// next consumes and returns the current token. EOF is sticky: advancing past it
// is a no-op, so parse helpers never index out of range.
func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}

func (p *parser) atEnd() bool { return p.toks[p.pos].kind == tEOF }

func parse(src string) (exprNode, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	node, err := p.parseCoalesce()
	if err != nil {
		return nil, err
	}
	if !p.atEnd() {
		return nil, &SyntaxError{
			Msg:     "unexpected token after expression",
			AppTier: isAppTierTrailing(p.peek().kind),
		}
	}
	return node, nil
}

// isAppTierTrailing reports whether a token left over after a complete
// expression indicates valid-PHP-beyond-v1 (arithmetic/word operators) rather
// than plain garbage (a stray bracket).
func isAppTierTrailing(k tokKind) bool {
	return k == tMinus || k == tAppTier || k == tIdent || k == tDoubleArrow
}

func (p *parser) parseCoalesce() (exprNode, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == tCoalesce {
		p.next()
		right, err := p.parseCoalesce() // right-associative
		if err != nil {
			return nil, err
		}
		return &coalesceNode{left: left, right: right}, nil
	}
	return left, nil
}

func (p *parser) parseOr() (exprNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &logicalNode{op: "||", left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (exprNode, error) {
	left, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		p.next()
		right, err := p.parseCompare()
		if err != nil {
			return nil, err
		}
		left = &logicalNode{op: "&&", left: left, right: right}
	}
	return left, nil
}

func compareOp(k tokKind) (string, bool) {
	switch k {
	case tEq:
		return "==", true
	case tNe:
		return "!=", true
	case tLt:
		return "<", true
	case tGt:
		return ">", true
	case tLe:
		return "<=", true
	case tGe:
		return ">=", true
	}
	return "", false
}

func (p *parser) parseCompare() (exprNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	op, ok := compareOp(p.peek().kind)
	if !ok {
		return left, nil
	}
	p.next()
	right, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if _, chained := compareOp(p.peek().kind); chained {
		return nil, &SyntaxError{Msg: "comparison operators are non-associative", AppTier: false}
	}
	return &compareNode{op: op, left: left, right: right}, nil
}

func (p *parser) parseUnary() (exprNode, error) {
	switch p.peek().kind {
	case tNot:
		p.next()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "!", operand: operand}, nil
	case tMinus:
		p.next()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "-", operand: operand}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (exprNode, error) {
	node, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case tArrow:
			p.next()
			nt := p.peek()
			if nt.kind != tIdent {
				if nt.kind == tVar {
					return nil, &SyntaxError{Msg: "dynamic property access (->$prop)", AppTier: true}
				}
				return nil, &SyntaxError{Msg: "expected property name after '->'", AppTier: false}
			}
			p.next()
			if p.peek().kind == tLParen {
				args, err := p.parseCallArgs()
				if err != nil {
					return nil, err
				}
				node = &callNode{recv: node, name: nt.text, args: args}
				continue
			}
			node = &propNode{recv: node, name: nt.text}
		case tLBracket:
			p.next()
			idx, err := p.parseCoalesce()
			if err != nil {
				return nil, err
			}
			if p.peek().kind != tRBracket {
				return nil, &SyntaxError{Msg: "expected ']'", AppTier: false}
			}
			p.next()
			node = &indexNode{recv: node, index: idx}
		default:
			return node, nil
		}
	}
}

// parseCallArgs parses a method call's parenthesized argument list, cursor on
// the '('. Arguments are full expressions; a trailing comma is tolerated
// (valid PHP since 7.3).
func (p *parser) parseCallArgs() ([]exprNode, error) {
	p.next() // '('
	var args []exprNode
	for p.peek().kind != tRParen {
		a, err := p.parseCoalesce()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if p.peek().kind == tComma {
			p.next()
			continue
		}
		break
	}
	if p.peek().kind != tRParen {
		return nil, &SyntaxError{Msg: "expected ')' in argument list", AppTier: false}
	}
	p.next()
	return args, nil
}

// parseArrayLiteral parses ['a', 'k' => $v, …], cursor on the '['. Entries
// are full expressions with an optional `key =>` prefix; a trailing comma is
// tolerated.
func (p *parser) parseArrayLiteral() (exprNode, error) {
	p.next() // '['
	node := &arrayNode{}
	for p.peek().kind != tRBracket {
		first, err := p.parseCoalesce()
		if err != nil {
			return nil, err
		}
		entry := arrayEntryNode{val: first}
		if p.peek().kind == tDoubleArrow {
			p.next()
			val, err := p.parseCoalesce()
			if err != nil {
				return nil, err
			}
			entry = arrayEntryNode{key: first, val: val}
		}
		node.entries = append(node.entries, entry)
		if p.peek().kind == tComma {
			p.next()
			continue
		}
		break
	}
	if p.peek().kind != tRBracket {
		return nil, &SyntaxError{Msg: "expected ']' in array literal", AppTier: false}
	}
	p.next()
	return node, nil
}

func (p *parser) parsePrimary() (exprNode, error) {
	t := p.peek()
	switch t.kind {
	case tLBracket:
		return p.parseArrayLiteral()
	case tDoubleArrow:
		p.next()
		return nil, &SyntaxError{Msg: "array arrow (=>)", AppTier: true}
	case tVar:
		p.next()
		return &varNode{name: t.text}, nil
	case tInt, tFloat:
		p.next()
		return &litNode{val: t.num}, nil
	case tStr:
		p.next()
		return &litNode{val: t.text}, nil
	case tTrue:
		p.next()
		return &litNode{val: true}, nil
	case tFalse:
		p.next()
		return &litNode{val: false}, nil
	case tNull:
		p.next()
		return &litNode{val: nil}, nil
	case tLParen:
		p.next()
		inner, err := p.parseCoalesce()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			return nil, &SyntaxError{Msg: "expected ')'", AppTier: false}
		}
		p.next()
		return inner, nil
	case tIdent:
		p.next()
		if p.peek().kind == tLParen {
			return nil, &SyntaxError{Msg: "function call " + t.text + "()", AppTier: true}
		}
		// A bare identifier is a constant/class reference — real PHP only.
		return nil, &SyntaxError{Msg: "bare identifier '" + t.text + "'", AppTier: true}
	case tAppTier:
		p.next()
		return nil, &SyntaxError{Msg: t.text, AppTier: true}
	case tEOF:
		return nil, &SyntaxError{Msg: "unexpected end of expression", AppTier: false}
	default:
		p.next()
		return nil, &SyntaxError{Msg: "unexpected token", AppTier: false}
	}
}

// ---------------------------------------------------------------------------
// Evaluation
// ---------------------------------------------------------------------------

func evalNode(node exprNode, scope Scope) (Value, error) {
	switch n := node.(type) {
	case *litNode:
		return n.val, nil
	case *varNode:
		if scope != nil {
			if v, ok := scope.Lookup(n.name); ok {
				return v, nil
			}
		}
		return nil, &UndefinedVarError{Name: n.name}
	case *propNode:
		recv, err := evalNode(n.recv, scope)
		if err != nil {
			return nil, err
		}
		return propAccess(recv, n.name)
	case *indexNode:
		recv, err := evalNode(n.recv, scope)
		if err != nil {
			return nil, err
		}
		idx, err := evalNode(n.index, scope)
		if err != nil {
			return nil, err
		}
		return indexAccess(recv, idx)
	case *arrayNode:
		arr := NewArr()
		for _, e := range n.entries {
			v, err := evalNode(e.val, scope)
			if err != nil {
				return nil, err
			}
			if e.key == nil {
				arr.Push(v)
				continue
			}
			k, err := evalNode(e.key, scope)
			if err != nil {
				return nil, err
			}
			switch k.(type) {
			case nil, bool, int64, float64, string:
				arr.SetKey(k, v)
			default:
				// PHP: "Illegal offset type" TypeError for an array/object key.
				return nil, errors.New("illegal array key type")
			}
		}
		return arr, nil
	case *callNode:
		recv, err := evalNode(n.recv, scope)
		if err != nil {
			// A failing receiver folds into the same app-tier classification a
			// method call reported before calls were modeled at all: whatever
			// the receiver is, ->name() on it is real PHP the engine either
			// runs (a bag) or hands to the app tier — a broken receiver never
			// downgrades that to a hard template error.
			return nil, &SyntaxError{Msg: "method call (->" + n.name + "())", AppTier: true}
		}
		bag, ok := recv.(*AttrBag)
		if !ok {
			return nil, &SyntaxError{Msg: "method call (->" + n.name + "())", AppTier: true}
		}
		args := make([]Value, len(n.args))
		for i, a := range n.args {
			v, err := evalNode(a, scope)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		return callBagMethod(bag, n.name, args)
	case *unaryNode:
		return evalUnary(n, scope)
	case *compareNode:
		return evalCompare(n, scope)
	case *logicalNode:
		return evalLogical(n, scope)
	case *coalesceNode:
		lv, err := evalNode(n.left, scope)
		if err != nil {
			// `??` uses isset semantics: an undefined variable OR a missing
			// object member short-circuits to the fallback. ErrObjectAsArray is
			// deliberately NOT caught here — isset on object-as-array itself
			// throws in PHP, so it propagates (oracle-probed).
			if errors.Is(err, ErrUndefinedVar) || errors.Is(err, ErrUndefinedProp) {
				return evalNode(n.right, scope)
			}
			return nil, err
		}
		if lv == nil {
			return evalNode(n.right, scope)
		}
		return lv, nil
	}
	return nil, &SyntaxError{Msg: "invalid expression node", AppTier: false}
}

// propAccess models `->member` reads. On an *Object a present member yields its
// value; a missing one is ErrUndefinedProp (unless it is a known app-tier member,
// e.g. $loop->remaining, which reports an app-tier SyntaxError). A map keeps its
// null-on-missing behavior, and a property on nil/scalar/array yields nil (PHP's
// null-propagation, left silent here as before).
func propAccess(recv Value, name string) (Value, error) {
	switch r := recv.(type) {
	case *Object:
		if v, ok := r.vals[name]; ok {
			return v, nil
		}
		if r.appTier[name] {
			return nil, &SyntaxError{Msg: "member ->" + name + " requires the app tier", AppTier: true}
		}
		return nil, &UndefinedPropError{Name: name}
	case map[string]Value:
		return r[name], nil
	default:
		return nil, nil
	}
}

// indexAccess models `[index]` reads. Applying it to an *Object is
// ErrObjectAsArray (PHP's "Cannot use object of type stdClass as array" fatal);
// lists and maps read the element the PHP way; anything else null-propagates.
func indexAccess(recv, idx Value) (Value, error) {
	switch r := recv.(type) {
	case *Object:
		return nil, ErrObjectAsArray
	case *Arr:
		v, _ := r.Lookup(idx) // missing key → nil, like a map read
		return v, nil
	case *AttrBag:
		// The bag implements ArrayAccess: $attributes['class'] reads get().
		if s, ok := idx.(string); ok {
			v, _ := r.Get(s)
			return v, nil
		}
		return nil, nil
	case []Value:
		i, ok := toArrayIndex(idx)
		if !ok || i < 0 || i >= len(r) {
			return nil, nil
		}
		return r[i], nil
	case map[string]Value:
		return r[toStringKey(idx)], nil
	default:
		return nil, nil
	}
}

// toArrayIndex coerces an index value to a list offset the PHP way (integers,
// integral floats, and integer-valued strings). Non-integer keys don't index a
// list, so ok is false and the caller yields nil.
func toArrayIndex(idx Value) (int, bool) {
	switch x := idx.(type) {
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		if v, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return v, true
		}
		return 0, false
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// toStringKey coerces a value to a PHP-ish array key for map access.
func toStringKey(idx Value) string {
	switch x := idx.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatInt(int64(x), 10) // PHP casts float keys to int
	case bool:
		if x {
			return "1"
		}
		return "0"
	case nil:
		return ""
	default:
		return ""
	}
}

func evalUnary(n *unaryNode, scope Scope) (Value, error) {
	v, err := evalNode(n.operand, scope)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "!":
		return !Truthy(v), nil
	case "-":
		switch x := v.(type) {
		case int64:
			return -x, nil
		case float64:
			return -x, nil
		case string:
			if f, ok := phpNumeric(x); ok {
				t := strings.TrimSpace(x)
				if !strings.ContainsAny(t, ".eE") {
					if iv, ierr := strconv.ParseInt(t, 10, 64); ierr == nil {
						return -iv, nil
					}
				}
				return -f, nil
			}
			return nil, fmt.Errorf("unary minus on non-numeric string %q", x)
		default:
			return nil, fmt.Errorf("unary minus on unsupported operand")
		}
	}
	return nil, &SyntaxError{Msg: "invalid unary operator", AppTier: false}
}

func evalLogical(n *logicalNode, scope Scope) (Value, error) {
	lv, err := evalNode(n.left, scope)
	if err != nil {
		return nil, err
	}
	lt := Truthy(lv)
	switch n.op {
	case "&&":
		if !lt {
			return false, nil
		}
	case "||":
		if lt {
			return true, nil
		}
	}
	rv, err := evalNode(n.right, scope)
	if err != nil {
		return nil, err
	}
	return Truthy(rv), nil
}

func evalCompare(n *compareNode, scope Scope) (Value, error) {
	lv, err := evalNode(n.left, scope)
	if err != nil {
		return nil, err
	}
	rv, err := evalNode(n.right, scope)
	if err != nil {
		return nil, err
	}
	// An object compared with a number fatals in PHP ("Object of class stdClass
	// could not be converted to int/float"), so surface an error rather than
	// silently succeed. Object-vs-null/bool/string/array/object are non-fatal and
	// handled by LooseEq below. The $attributes bag is an object too.
	if (isObjecty(lv) && isNumber(rv)) || (isNumber(lv) && isObjecty(rv)) {
		return nil, errObjectNumber
	}
	switch n.op {
	case "==":
		return LooseEq(lv, rv), nil
	case "!=":
		return !LooseEq(lv, rv), nil
	}
	c, err := compareOrder(lv, rv)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "<":
		return c < 0, nil
	case "<=":
		return c <= 0, nil
	case ">":
		return c > 0, nil
	case ">=":
		return c >= 0, nil
	}
	return nil, &SyntaxError{Msg: "invalid comparison operator", AppTier: false}
}

// ---------------------------------------------------------------------------
// PHP value semantics
// ---------------------------------------------------------------------------

// Truthy reports PHP truthiness. Falsy: false, 0, 0.0, "", "0", nil, empty
// array, empty map. Everything else is truthy.
func Truthy(v Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != "" && x != "0"
	case []Value:
		return len(x) != 0
	case map[string]Value:
		return len(x) != 0
	case *Arr:
		return x.Len() != 0
	case *Object:
		// A PHP object is ALWAYS truthy, even an empty stdClass — unlike an empty
		// array/map. This is the flip that gives `@if ($site)` with `{}` parity.
		return true
	case *AttrBag:
		return true // an object, hence always truthy
	default:
		return true
	}
}

// LooseEq implements PHP 8 loose equality (==).
func LooseEq(a, b Value) bool {
	if a == nil && b == nil {
		return true
	}
	// If either side is a bool, both are compared as bools.
	if isBool(a) || isBool(b) {
		return Truthy(a) == Truthy(b)
	}
	// null vs non-bool: null equals the "empty" value of the other type.
	if a == nil {
		return emptyForNull(b)
	}
	if b == nil {
		return emptyForNull(a)
	}
	as, aIsStr := a.(string)
	bs, bIsStr := b.(string)
	switch {
	case isNumber(a) && isNumber(b):
		return numEqual(a, b)
	case isNumber(a) && bIsStr:
		if f, ok := phpNumeric(bs); ok {
			return toFloat(a) == f
		}
		return false
	case aIsStr && isNumber(b):
		if f, ok := phpNumeric(as); ok {
			return f == toFloat(b)
		}
		return false
	case aIsStr && bIsStr:
		fa, aok := phpNumeric(as)
		fb, bok := phpNumeric(bs)
		if aok && bok {
			return fa == fb
		}
		return as == bs
	case isObject(a) && isObject(b):
		return objectEq(a.(*Object), b.(*Object))
	case isBag(a) && isBag(b):
		return bagEq(a.(*AttrBag), b.(*AttrBag))
	case isArrayish(a) && isArrayish(b):
		return arrEq(asArr(a), asArr(b))
	default:
		// Object vs array/string (and array vs scalar) are unequal in PHP 8 with
		// no fatal. Object vs a number DOES fatal in PHP; evalCompare intercepts
		// that pair before reaching here, so a direct LooseEq call falls through
		// to false.
		return false
	}
}

// objectEq is PHP `==` on two stdClass values: equal iff they have the same set
// of properties (order-independent) and every property value is loosely equal,
// recursively. Oracle-probed: key order does not matter; nested values compare
// with `==`; two empty objects are equal.
func objectEq(a, b *Object) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, k := range a.keys {
		bv, ok := b.Get(k)
		if !ok {
			return false
		}
		if !LooseEq(a.vals[k], bv) {
			return false
		}
	}
	return true
}

// compareOrder returns -1/0/1 for a<b/a==b/a>b under PHP 8 relational rules, or
// errArrayOrder when an array/map is involved.
func compareOrder(a, b Value) (int, error) {
	if isArrayish(a) || isArrayish(b) {
		return 0, errArrayOrder
	}
	if isObjecty(a) || isObjecty(b) {
		return 0, errObjectOrder
	}
	if isBool(a) || isBool(b) {
		return cmpFloat(boolToFloat(Truthy(a)), boolToFloat(Truthy(b))), nil
	}
	af, aNum := orderNumeric(a)
	bf, bNum := orderNumeric(b)
	if aNum && bNum {
		return cmpFloat(af, bf), nil
	}
	return strings.Compare(phpString(a), phpString(b)), nil
}

// orderNumeric reports the float form of a value that participates in numeric
// ordering — a real number or a numeric string.
func orderNumeric(v Value) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case string:
		return phpNumeric(x)
	default:
		return 0, false
	}
}

func numEqual(a, b Value) bool {
	if ai, aok := a.(int64); aok {
		if bi, bok := b.(int64); bok {
			return ai == bi
		}
	}
	return toFloat(a) == toFloat(b)
}

// asArr views any arrayish value as an ordered *Arr for key-wise comparison
// (PHP == on arrays is key-based and order-insensitive, so the map's
// undefined iteration order does not matter here).
func asArr(v Value) *Arr {
	switch x := v.(type) {
	case *Arr:
		return x
	case []Value:
		a := NewArr()
		for _, e := range x {
			a.Push(e)
		}
		return a
	case map[string]Value:
		a := NewArr()
		for k, val := range x {
			a.SetKey(k, val)
		}
		return a
	}
	return NewArr()
}

// bagEq is PHP `==` on two ComponentAttributeBag objects: same class, so equal
// iff their attribute arrays are loosely equal.
func bagEq(a, b *AttrBag) bool {
	if len(a.entries) != len(b.entries) {
		return false
	}
	for _, en := range a.entries {
		bv, ok := b.Get(en.Name)
		if !ok || !LooseEq(en.Val, bv) {
			return false
		}
	}
	return true
}

// emptyForNull reports whether v equals PHP null under ==. bool and nil are
// handled by the caller before this is reached.
func emptyForNull(v Value) bool {
	switch x := v.(type) {
	case int64:
		return x == 0
	case float64:
		return x == 0
	case string:
		return x == ""
	case []Value:
		return len(x) == 0
	case map[string]Value:
		return len(x) == 0
	case *Arr:
		return x.Len() == 0
	case *Object, *AttrBag:
		// An object never equals null under `==` (probed: `$obj == null` false),
		// even when empty — the object/array truthiness asymmetry again.
		return false
	default:
		return false
	}
}

// phpNumeric reports whether s is a PHP numeric string (leading and trailing
// whitespace allowed, PHP 8 style) and returns its float value. It is a
// hand-written scan — no regex.
func phpNumeric(s string) (float64, bool) {
	t := strings.TrimFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
	})
	if t == "" {
		return 0, false
	}
	i, n := 0, len(t)
	if t[i] == '+' || t[i] == '-' {
		i++
	}
	digitsBefore, digitsAfter := 0, 0
	for i < n && t[i] >= '0' && t[i] <= '9' {
		i++
		digitsBefore++
	}
	if i < n && t[i] == '.' {
		i++
		for i < n && t[i] >= '0' && t[i] <= '9' {
			i++
			digitsAfter++
		}
	}
	if digitsBefore == 0 && digitsAfter == 0 {
		return 0, false
	}
	if i < n && (t[i] == 'e' || t[i] == 'E') {
		i++
		if i < n && (t[i] == '+' || t[i] == '-') {
			i++
		}
		expDigits := 0
		for i < n && t[i] >= '0' && t[i] <= '9' {
			i++
			expDigits++
		}
		if expDigits == 0 {
			return 0, false
		}
	}
	if i != n {
		return 0, false
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func phpString(v Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "1"
		}
		return ""
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return x
	default:
		return ""
	}
}

func toFloat(v Value) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func isBool(v Value) bool {
	_, ok := v.(bool)
	return ok
}

func isNumber(v Value) bool {
	switch v.(type) {
	case int64, float64:
		return true
	}
	return false
}

func isArrayish(v Value) bool {
	switch v.(type) {
	case []Value, map[string]Value, *Arr:
		return true
	}
	return false
}

func isObject(v Value) bool {
	_, ok := v.(*Object)
	return ok
}

func isBag(v Value) bool {
	_, ok := v.(*AttrBag)
	return ok
}

// isObjecty groups every value PHP treats as an object for the fatal
// object-vs-number comparison paths: stdClass and the $attributes bag.
func isObjecty(v Value) bool {
	return isObject(v) || isBag(v)
}
