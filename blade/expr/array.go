package expr

import "strconv"

// Arr is an ordered PHP array value: the result of evaluating an array
// literal (['a', 'k' => $v]). It preserves insertion order and PHP's key
// model — int|string keys, with PHP's key-cast rules and auto-indexing —
// which []Value (a pure list) and map[string]Value (unordered) cannot carry.
// It exists so the $attributes bag methods and the @class/@style directives
// receive their argument arrays with faithful key/order semantics.
type Arr struct {
	entries []ArrPair
	nextIdx int64 // the next auto-index a keyless entry receives
}

// ArrPair is one ordered array entry. Key is int64 or string, already
// normalized by PHP's key-cast rules (see normalizeArrayKey).
type ArrPair struct {
	Key Value
	Val Value
}

// NewArr returns an empty ordered array.
func NewArr() *Arr {
	return &Arr{}
}

// normalizeArrayKey applies PHP's array-key cast: bool → 0/1, float →
// truncated int, null → "", and a CANONICAL decimal-integer string ("5",
// "-3" — but not "05", "5.0", or " 5") → int. Everything else keys as the
// string itself.
func normalizeArrayKey(k Value) Value {
	switch x := k.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case bool:
		if x {
			return int64(1)
		}
		return int64(0)
	case nil:
		return ""
	case string:
		if n, ok := canonicalInt(x); ok {
			return n
		}
		return x
	default:
		return ""
	}
}

// canonicalInt reports whether s is PHP's canonical decimal form of an
// integer key (what (string)(int)$s round-trips to): optional '-', no
// leading zeros (except "0" itself), fits int64.
func canonicalInt(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	body := s
	if s[0] == '-' {
		body = s[1:]
	}
	if body == "" {
		return 0, false
	}
	if len(body) > 1 && body[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// SetKey sets key → val, PHP style: an existing key keeps its position and
// takes the new value; a new key appends. An int key at or above the auto
// index advances it.
func (a *Arr) SetKey(key, val Value) {
	k := normalizeArrayKey(key)
	if n, ok := k.(int64); ok && n >= a.nextIdx {
		a.nextIdx = n + 1
	}
	for i := range a.entries {
		if a.entries[i].Key == k {
			a.entries[i].Val = val
			return
		}
	}
	a.entries = append(a.entries, ArrPair{Key: k, Val: val})
}

// Push appends val under the next auto index (PHP's $arr[] = $val).
func (a *Arr) Push(val Value) {
	a.SetKey(a.nextIdx, val)
}

// Len returns the number of entries.
func (a *Arr) Len() int {
	return len(a.entries)
}

// Lookup reads a key (normalized first). Missing keys return (nil, false).
func (a *Arr) Lookup(key Value) (Value, bool) {
	k := normalizeArrayKey(key)
	for i := range a.entries {
		if a.entries[i].Key == k {
			return a.entries[i].Val, true
		}
	}
	return nil, false
}

// Pairs returns the entries in insertion order (a copy of the slice header;
// callers must not mutate the pairs).
func (a *Arr) Pairs() []ArrPair {
	return a.entries
}

// StringValues flattens the array's VALUES to strings — the shape bag
// methods like only()/except()/whereStartsWith() take their key lists in.
// Non-scalar values are skipped (PHP would notice-and-cast; templates never
// hit this).
func (a *Arr) StringValues() []string {
	out := make([]string, 0, len(a.entries))
	for _, p := range a.entries {
		switch p.Val.(type) {
		case string, int64, float64, bool:
			out = append(out, phpString(p.Val))
		}
	}
	return out
}

// arrEq is PHP `==` on two ordered arrays: equal iff same size and every key
// of a exists in b with a loosely equal value (order-insensitive).
func arrEq(a, b *Arr) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, p := range a.entries {
		bv, ok := b.Lookup(p.Key)
		if !ok || !LooseEq(p.Val, bv) {
			return false
		}
	}
	return true
}
