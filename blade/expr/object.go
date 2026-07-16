package expr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Object is an order-preserving PHP-stdClass-like object. JSON objects decode to
// *Object (see DecodeJSON) and every stdClass the SiteData contract binds — $site,
// each object item inside a collection array, each content item, and $loop — is
// carried as one. It is DISTINCT from map[string]Value, which keeps PHP
// associative-array semantics; the split is what lets the engine mirror real
// Blade, where json_decode without assoc yields stdClass:
//
//   - Truthy(*Object) is ALWAYS true, even for an empty object (a PHP empty
//     stdClass is truthy where an empty array is falsy).
//   - `->member` reads a property; a MISSING member is ErrUndefinedProp (which
//     `??`/isset suppresses, matching real Blade's Undefined-property warning).
//   - `[index]` is ErrObjectAsArray ("Cannot use object of type stdClass as
//     array") — a fatal that `??`/isset does NOT suppress.
//   - `==` is order-independent, recursive, property-wise (objectEq).
//
// Iteration and equality follow the object's stored key order, which is the JSON
// document's insertion order (PHP objects iterate properties in insertion order).
type Object struct {
	keys []string
	vals map[string]Value
	// appTier names members that are valid Blade but out of v1's subset, so a
	// bare access reports app-tier rather than error/undefined-property. Only
	// $loop uses it (remaining/depth/parent — spec §3.2). Nil for data objects.
	appTier map[string]bool
}

// NewObject returns an empty, ready-to-populate object.
func NewObject() *Object {
	return &Object{vals: map[string]Value{}}
}

// Set assigns key=val. A new key is appended to the iteration order; re-assigning
// an existing key keeps its original position and replaces the value — exactly
// PHP's array/object assignment semantics (used for JSON/frontmatter duplicates).
func (o *Object) Set(key string, val Value) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// Get returns the value for key and whether the property is present.
func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Keys returns the property names in insertion order. The slice is the object's
// own backing array — callers must not mutate it.
func (o *Object) Keys() []string { return o.keys }

// Len is the number of properties.
func (o *Object) Len() int { return len(o.keys) }

// MarkAppTier records members that resolve to an app-tier signal when missing
// (see the appTier field). Safe to call more than once.
func (o *Object) MarkAppTier(names ...string) {
	if o.appTier == nil {
		o.appTier = make(map[string]bool, len(names))
	}
	for _, n := range names {
		o.appTier[n] = true
	}
}

// DecodeJSON parses one JSON document into a Value with ORDER PRESERVED: a JSON
// object becomes an *Object whose keys iterate in document order, a JSON array
// becomes a []Value, and scalars map to the same Go types json.Unmarshal into
// `any` produces (numbers → float64, so Stringify prints an integral JSON number
// without a fractional part, exactly as PHP echoes a json_decode int). It is the
// single decoder every file-sourced producer ($site, collections) uses, so their
// objects gain stdClass semantics uniformly. Trailing bytes after the first
// document (other than whitespace) are a syntax error, matching json.Unmarshal's
// whole-document strictness.
func DecodeJSON(data []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	v, err := decodeFromToken(dec, tok)
	if err != nil {
		return nil, err
	}
	// Reject trailing tokens (e.g. `{"a":1} 2`) so DecodeJSON is as strict about
	// a whole document as json.Unmarshal is.
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON after top-level value")
		}
		return nil, err
	}
	return v, nil
}

// decodeValue reads the next value (scalar, object, or array) from dec.
func decodeValue(dec *json.Decoder) (Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeFromToken(dec, tok)
}

// decodeFromToken builds a Value from an already-read opening token, recursing
// into objects and arrays. Objects preserve key order; arrays preserve element
// order; scalars pass through as the json.Token Go types.
func decodeFromToken(dec *json.Decoder, tok json.Token) (Value, error) {
	if d, ok := tok.(json.Delim); ok {
		switch d {
		case '{':
			obj := NewObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []Value{}
			for dec.More() {
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected JSON delimiter %q", d)
	}
	// Scalars: json.Token yields nil, bool, float64, or string here.
	switch v := tok.(type) {
	case nil:
		return nil, nil
	case bool:
		return v, nil
	case float64:
		return v, nil
	case string:
		return v, nil
	}
	return nil, fmt.Errorf("unexpected JSON token %T", tok)
}
