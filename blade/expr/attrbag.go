package expr

import (
	"errors"
	"strings"

	"github.com/thedevdojo/pocketknife/blade/internal/phpesc"
)

// AttrBag models Laravel's Illuminate\View\ComponentAttributeBag: the ordered
// name → value map a component receives as $attributes (every tag attribute
// not consumed by @props). It is a first-class expr Value so the evaluator
// can dispatch the bag's method subset (merge, class, only, …); rendering it
// to HTML (HTMLString) mirrors the PHP __toString byte-for-byte.
//
// Methods that take a closure (filter, map, when*, …), prepends(), and
// anything else outside the modeled subset report an app-tier SyntaxError —
// valid Blade the engine hands to the PHP app tier, exactly like any other
// unsupported construct.
type AttrBag struct {
	entries []AttrEntry
}

// AttrEntry is one attribute: the name as written on the tag and its value
// (string for static/interpolated attributes, any Value for bound ones,
// bool true for a valueless attribute).
type AttrEntry struct {
	Name string
	Val  Value
}

// errBagNotStringable reports a bag attribute whose value PHP could not cast
// to a string while imploding the bag (an array/object value) — a fatal in
// real Blade, a render diagnostic here.
var errBagNotStringable = errors.New("attribute value cannot be cast to a string")

// NewAttrBag builds a bag from ordered entries, PHP-array style: a repeated
// name overwrites in place. It also models setAttributes' special case — an
// entry literally named `attributes` holding another bag folds that parent
// bag in via parent.merge(remaining, escape=false), which is how real Blade
// supports passing a whole bag through (<x-b :attributes="$attributes"/>).
func NewAttrBag(entries []AttrEntry) *AttrBag {
	b := &AttrBag{}
	var parent *AttrBag
	for _, en := range entries {
		if en.Name == "attributes" {
			if pb, ok := en.Val.(*AttrBag); ok {
				parent = pb
				continue
			}
		}
		b.set(en.Name, en.Val)
	}
	if parent != nil {
		merged, err := parent.merge(b.entries, false)
		if err == nil {
			return merged
		}
		// A merge failure (unstringable class/style value) leaves the parent
		// out; the bad value will be diagnosed when the bag renders.
	}
	return b
}

// set writes name → val, keeping an existing name's position (PHP array
// assignment semantics).
func (b *AttrBag) set(name string, val Value) {
	for i := range b.entries {
		if b.entries[i].Name == name {
			b.entries[i].Val = val
			return
		}
	}
	b.entries = append(b.entries, AttrEntry{Name: name, Val: val})
}

// Get reads one attribute. ok reports presence; a present attribute may hold
// nil (rendered as nothing, but has() still sees it).
func (b *AttrBag) Get(name string) (Value, bool) {
	for _, en := range b.entries {
		if en.Name == name {
			return en.Val, true
		}
	}
	return nil, false
}

// Entries returns the ordered attributes (callers must not mutate).
func (b *AttrBag) Entries() []AttrEntry {
	return b.entries
}

// HTMLString implodes the bag exactly like ComponentAttributeBag::__toString:
// false/null attributes are skipped; true renders the attribute name as its
// value (except x-data and wire:* which render empty); every value is
// PHP-trimmed and its double quotes become \" (PHP's str_replace('"','\\"')
// — no HTML escaping happens here; bound values were escaped when the bag
// was built). A non-scalar value is PHP's "Array to string conversion" fatal
// and returns an error.
func (b *AttrBag) HTMLString() (string, error) {
	var sb strings.Builder
	for _, en := range b.entries {
		v := en.Val
		if v == nil {
			continue
		}
		if bv, ok := v.(bool); ok {
			if !bv {
				continue
			}
			if en.Name == "x-data" || strings.HasPrefix(en.Name, "wire:") {
				v = ""
			} else {
				v = en.Name
			}
		}
		s, err := bagValueString(v)
		if err != nil {
			return "", err
		}
		sb.WriteString(" ")
		sb.WriteString(en.Name)
		sb.WriteString(`="`)
		sb.WriteString(strings.ReplaceAll(phpTrimString(s), `"`, `\"`))
		sb.WriteString(`"`)
	}
	// The PHP side ends with trim($string); construction only ever adds a
	// single leading space, so stripping the trim mask from both ends matches.
	return phpTrimString(sb.String()), nil
}

// bagValueString casts a scalar attribute value to string the PHP way;
// arrays/maps/objects/bags cannot be cast (fatal in PHP).
func bagValueString(v Value) (string, error) {
	switch v.(type) {
	case nil, bool, int64, float64, string:
		return phpString(v), nil
	}
	return "", errBagNotStringable
}

// phpTrimString strips PHP trim()'s default mask (" \t\n\r\0\x0B") from both
// ends — the exact set __toString applies to each value and to the result.
func phpTrimString(s string) string {
	isMask := func(b byte) bool {
		switch b {
		case ' ', '\t', '\n', '\r', 0, '\v':
			return true
		}
		return false
	}
	i := 0
	for i < len(s) && isMask(s[i]) {
		i++
	}
	j := len(s)
	for j > i && isMask(s[j-1]) {
		j--
	}
	return s[i:j]
}

// ---------------------------------------------------------------------------
// Method dispatch
// ---------------------------------------------------------------------------

// callBagMethod evaluates $attributes->name(args...). PHP method names are
// case-insensitive, so dispatch lowercases. Methods whose real signature
// takes a closure — and any name outside the modeled subset (the bag is
// Macroable) — return an app-tier SyntaxError.
func callBagMethod(b *AttrBag, name string, args []Value) (Value, error) {
	switch strings.ToLower(name) {
	case "merge":
		defaults, err := argEntries(args, 0)
		if err != nil {
			return nil, err
		}
		escape := true
		if len(args) > 1 {
			escape = Truthy(args[1])
		}
		return b.merge(defaults, escape)
	case "class":
		classes, err := ToCssClasses(argOr(args, 0, nil))
		if err != nil {
			return nil, err
		}
		return b.merge([]AttrEntry{{Name: "class", Val: classes}}, true)
	case "style":
		styles, err := ToCssStyles(argOr(args, 0, nil))
		if err != nil {
			return nil, err
		}
		return b.merge([]AttrEntry{{Name: "style", Val: styles}}, true)
	case "only":
		keys := argStrings(args)
		out := &AttrBag{}
		for _, en := range b.entries {
			if containsString(keys, en.Name) {
				out.set(en.Name, en.Val)
			}
		}
		return out, nil
	case "except":
		keys := argStrings(args)
		out := &AttrBag{}
		for _, en := range b.entries {
			if !containsString(keys, en.Name) {
				out.set(en.Name, en.Val)
			}
		}
		return out, nil
	case "has":
		for _, key := range argStrings(args) {
			if _, ok := b.Get(key); !ok {
				return false, nil
			}
		}
		return true, nil
	case "hasany":
		for _, key := range argStrings(args) {
			if _, ok := b.Get(key); ok {
				return true, nil
			}
		}
		return false, nil
	case "missing":
		for _, key := range argStrings(args) {
			if _, ok := b.Get(key); !ok {
				return true, nil
			}
		}
		return false, nil
	case "get":
		key, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		// PHP: $this->attributes[$key] ?? value($default) — a present-but-null
		// value still falls to the default (?? semantics).
		if v, ok := b.Get(key); ok && v != nil {
			return v, nil
		}
		return argOr(args, 1, nil), nil
	case "first":
		for _, en := range b.entries {
			if en.Val != nil {
				return en.Val, nil
			}
			// ArrayIterator::current() ?? value($default): a nil first value
			// falls to the default just like an empty bag.
			break
		}
		return argOr(args, 0, nil), nil
	case "wherestartswith", "thatstartwith":
		return b.filterByPrefix(argStrings(args), true), nil
	case "wheredoesntstartwith":
		return b.filterByPrefix(argStrings(args), false), nil
	case "isempty":
		s, err := b.HTMLString()
		if err != nil {
			return nil, err
		}
		return s == "", nil
	case "isnotempty":
		s, err := b.HTMLString()
		if err != nil {
			return nil, err
		}
		return s != "", nil
	case "all", "toarray", "getattributes", "jsonserialize":
		out := NewArr()
		for _, en := range b.entries {
			out.SetKey(en.Name, en.Val)
		}
		return out, nil
	case "filter", "map", "when", "unless", "whenhas", "whenmissing",
		"whenfilled", "whennotfilled", "prepends", "pipe", "tap":
		return nil, &SyntaxError{Msg: "$attributes->" + name + "() requires the PHP app tier", AppTier: true}
	default:
		// Unknown method: could be a Macroable extension — real PHP, not ours.
		return nil, &SyntaxError{Msg: "method call (->" + name + "())", AppTier: true}
	}
}

// merge mirrors ComponentAttributeBag::merge($attributeDefaults, $escape):
//
//  1. every default value is e()-escaped (strings and numbers; null/bool/
//     object pass through) when escape is true;
//  2. the bag's entries partition into appendable (class/style) and the rest;
//  3. each appendable entry combines default+own value — style values gain a
//     trailing ';' — joined by ' ' after PHP's array_filter (drops falsy) and
//     array_unique;
//  4. the result is array_merge(defaults, combined + rest): default keys keep
//     their position (values overridden by the combined bag where shared),
//     then bag-only keys in bag order.
func (b *AttrBag) merge(defaults []AttrEntry, escape bool) (*AttrBag, error) {
	esc := make([]AttrEntry, len(defaults))
	for i, d := range defaults {
		v := d.Val
		if escape {
			switch v.(type) {
			case string, int64, float64:
				v = phpesc.Escape(phpString(v))
			}
		}
		esc[i] = AttrEntry{Name: d.Name, Val: v}
	}
	defaultVal := func(name string) (Value, bool) {
		for _, d := range esc {
			if d.Name == name {
				return d.Val, true
			}
		}
		return nil, false
	}

	// Combined bag entries: appendable (class/style) results first, then the
	// non-appendable rest — the order Collection::partition + merge produces.
	var appendable, rest []AttrEntry
	for _, en := range b.entries {
		if en.Name == "class" || en.Name == "style" {
			dv, _ := defaultVal(en.Name)
			dvs := ""
			if dv != nil {
				s, err := bagValueString(dv)
				if err != nil {
					return nil, err
				}
				dvs = s
			}
			own, err := bagValueString(en.Val)
			if err != nil {
				return nil, err
			}
			if en.Name == "style" {
				own = strFinish(own, ";")
				if dvs != "" {
					dvs = strFinish(dvs, ";")
				}
			}
			// implode(' ', array_unique(array_filter([$defaultsValue, $value])))
			parts := make([]string, 0, 2)
			if Truthy(dvs) {
				parts = append(parts, dvs)
			}
			if Truthy(own) && (len(parts) == 0 || parts[0] != own) {
				parts = append(parts, own)
			}
			appendable = append(appendable, AttrEntry{Name: en.Name, Val: strings.Join(parts, " ")})
		} else {
			rest = append(rest, en)
		}
	}
	combined := append(appendable, rest...)
	combinedVal := func(name string) (Value, bool) {
		for _, c := range combined {
			if c.Name == name {
				return c.Val, true
			}
		}
		return nil, false
	}

	// array_merge($attributeDefaults, $attributes): default keys first (their
	// value replaced by the combined bag's where present), then the rest.
	out := &AttrBag{}
	for _, d := range esc {
		if v, ok := combinedVal(d.Name); ok {
			out.set(d.Name, v)
		} else {
			out.set(d.Name, d.Val)
		}
	}
	for _, c := range combined {
		if _, ok := defaultVal(c.Name); !ok {
			out.set(c.Name, c.Val)
		}
	}
	return out, nil
}

// filterByPrefix keeps (or drops) entries whose name starts with any needle.
func (b *AttrBag) filterByPrefix(needles []string, keep bool) *AttrBag {
	out := &AttrBag{}
	for _, en := range b.entries {
		matched := false
		for _, n := range needles {
			if n != "" && strings.HasPrefix(en.Name, n) {
				matched = true
				break
			}
		}
		if matched == keep {
			out.set(en.Name, en.Val)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Argument coercion helpers
// ---------------------------------------------------------------------------

// argOr returns args[i] or a fallback when absent.
func argOr(args []Value, i int, fallback Value) Value {
	if i < len(args) {
		return args[i]
	}
	return fallback
}

// argString requires args[i] to be a scalar and returns its PHP string form.
func argString(args []Value, i int) (string, error) {
	if i >= len(args) {
		return "", &SyntaxError{Msg: "missing argument", AppTier: false}
	}
	return bagValueString(args[i])
}

// argStrings flattens variadic string-or-array arguments into a name list —
// the is_array($key) ? $key : func_get_args() pattern the bag's methods share.
func argStrings(args []Value) []string {
	if len(args) == 1 {
		if arr, ok := args[0].(*Arr); ok {
			return arr.StringValues()
		}
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a.(type) {
		case string, int64, float64, bool:
			out = append(out, phpString(a))
		}
	}
	return out
}

// argEntries reads an optional *Arr argument as ordered name → value entries
// (merge()'s defaults). Int keys become their decimal names — array_merge
// would renumber them, which for the sequential keys a literal produces is
// the same digits. A non-array argument is a PHP TypeError.
func argEntries(args []Value, i int) ([]AttrEntry, error) {
	if i >= len(args) || args[i] == nil {
		return nil, nil
	}
	arr, ok := args[i].(*Arr)
	if !ok {
		return nil, errors.New("merge() expects an array")
	}
	out := make([]AttrEntry, 0, arr.Len())
	for _, p := range arr.Pairs() {
		name, err := bagValueString(p.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, AttrEntry{Name: name, Val: p.Val})
	}
	return out, nil
}

// strFinish mirrors Str::finish: exactly one trailing cap.
func strFinish(s, cap string) string {
	for strings.HasSuffix(s, cap) {
		s = s[:len(s)-len(cap)]
	}
	return s + cap
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// CSS class / style lists (Arr::toCssClasses / Arr::toCssStyles)
// ---------------------------------------------------------------------------

// ToCssClasses compiles a class list the way Arr::toCssClasses does: the
// input is Arr::wrap'd (nil → empty, a bare scalar → one entry); a numeric
// key contributes its VALUE unconditionally; a string key contributes the
// KEY when its value is truthy. Shared by $attributes->class() and the
// @class directive.
func ToCssClasses(v Value) (string, error) {
	return toCssList(v, false)
}

// ToCssStyles is Arr::toCssStyles: like ToCssClasses, but every kept rule
// gains exactly one trailing ';' (Str::finish).
func ToCssStyles(v Value) (string, error) {
	return toCssList(v, true)
}

func toCssList(v Value, finishSemi bool) (string, error) {
	var pairs []ArrPair
	switch x := v.(type) {
	case nil:
		// Arr::wrap(null) → []
	case *Arr:
		pairs = x.Pairs()
	default:
		pairs = []ArrPair{{Key: int64(0), Val: x}}
	}
	var out []string
	for _, p := range pairs {
		numericKey := false
		switch k := p.Key.(type) {
		case int64:
			numericKey = true
		case string:
			_, numericKey = phpNumeric(k)
		}
		if numericKey {
			s, err := bagValueString(p.Val)
			if err != nil {
				return "", err
			}
			if finishSemi {
				s = strFinish(s, ";")
			}
			out = append(out, s)
		} else if Truthy(p.Val) {
			key, err := bagValueString(p.Key)
			if err != nil {
				return "", err
			}
			if finishSemi {
				key = strFinish(key, ";")
			}
			out = append(out, key)
		}
	}
	return strings.Join(out, " "), nil
}
