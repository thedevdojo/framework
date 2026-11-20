// Package content is DevDojo Framework's markdown pipeline for content collections. It
// is the ONE place goldmark lives: the blade/* engine core stays stdlib-only, so
// the markdown dependency is confined here (spec §4). Given a set of `.md` files
// under src/data/content/<dir>/, it parses a scalar-only YAML frontmatter
// block, renders the body to HTML (goldmark, GFM tables + raw-HTML passthrough),
// and derives a `link` route — the shape the render scope binds as $<dir> items.
//
// The frontmatter grammar is a deliberate subset (scalars only: strings, ints,
// floats, true/false) parsed by hand — no YAML dependency — so the PHP tier
// (laravel/src/SiteData.php) can mirror it byte-for-byte. Anything nested (an
// indented child block, a flow array/map, a bare `key:`) is out of the subset
// and reported as ErrNested so the caller can skip the item with a diagnostic.
package content

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// ErrNested is returned by ParseFrontmatter when a value is not a scalar — an
// indented child block, a `[...]`/`{...}` flow collection, or a bare `key:` with
// no value. v1 content frontmatter is scalars only.
var ErrNested = errors.New("frontmatter value is not a scalar")

// md is the shared, configured markdown renderer. Table adds GFM tables;
// html.WithUnsafe lets raw HTML in the source pass through verbatim (a static
// site legitimately embeds raw blocks). It is safe for concurrent use.
var md = goldmark.New(
	goldmark.WithExtensions(extension.Table),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// Frontmatter is a parsed scalar frontmatter map plus the markdown body that
// follows it. Fields values are one of: string, int64, float64, bool. Order is
// the field keys in first-occurrence document order, so a content item's object
// iterates its properties the way real Blade's (object)$fields cast does.
type Frontmatter struct {
	Fields map[string]any
	Order  []string
	Body   string
}

// File is one markdown source file. Name is the base filename (e.g. "hello.md"),
// used for ordering and link derivation.
type File struct {
	Name string
	Data []byte
}

// Item is a rendered content-collection entry: the frontmatter Fields plus the
// rendered HTML (Content) and derived route (Link). The render scope turns this
// into an object with the Fields keys (in Order), "content", and "link".
type Item struct {
	Fields  map[string]any
	Order   []string
	Content string
	Link    string
}

// FileError pairs a source file name with the reason it was skipped.
type FileError struct {
	Name string
	Err  error
}

// RenderCollection renders md files into items ordered by Name ascending. dir is
// the collection directory (e.g. "post") used to derive each item's link
// ("post" + "hello.md" → "/post/hello"). A file whose frontmatter is non-scalar
// is skipped and reported in errs (order-independent of the returned items).
func RenderCollection(dir string, files []File) (items []Item, errs []FileError) {
	sorted := make([]File, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, f := range sorted {
		fm, err := ParseFrontmatter(f.Data)
		if err != nil {
			errs = append(errs, FileError{Name: f.Name, Err: err})
			continue
		}
		items = append(items, Item{
			Fields:  fm.Fields,
			Order:   fm.Order,
			Content: RenderMarkdown(fm.Body),
			Link:    link(dir, f.Name),
		})
	}
	return items, errs
}

// link derives a content route from a collection dir and a file name:
// ("post", "hello.md") → "/post/hello".
func link(dir, name string) string {
	slug := strings.TrimSuffix(name, ".md")
	return "/" + dir + "/" + slug
}

// RenderMarkdown converts a markdown body to HTML with goldmark (GFM tables,
// raw-HTML passthrough). goldmark never errors on a []byte source in practice;
// if it ever did, the partial buffer is returned rather than panicking.
func RenderMarkdown(source string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return buf.String()
	}
	return buf.String()
}

// ParseFrontmatter splits an optional leading `---` fence from src and parses its
// `key: value` lines into scalar values. A file that does not begin with a
// `---` fence (optionally after a UTF-8 BOM) has no frontmatter: Fields is empty
// and Body is the whole input. A non-scalar value yields ErrNested.
func ParseFrontmatter(src []byte) (Frontmatter, error) {
	s := string(src)
	// Tolerate a leading UTF-8 BOM (EF BB BF) the way most markdown tooling does.
	s = strings.TrimPrefix(s, "\uFEFF")

	open, ok := stripFence(s)
	if !ok {
		return Frontmatter{Fields: map[string]any{}, Body: string(src)}, nil
	}
	// Find the closing fence: a line that is exactly `---`.
	block, body, ok := splitClosingFence(open)
	if !ok {
		// Unterminated fence: treat as no frontmatter, whole file is the body.
		return Frontmatter{Fields: map[string]any{}, Body: string(src)}, nil
	}

	fields := map[string]any{}
	var order []string
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// An indented line has no top-level key: it is a nested child block.
		if line[0] == ' ' || line[0] == '\t' {
			return Frontmatter{}, ErrNested
		}
		key, rawVal, ok := strings.Cut(line, ":")
		if !ok {
			return Frontmatter{}, ErrNested
		}
		val, err := parseScalar(strings.TrimSpace(rawVal))
		if err != nil {
			return Frontmatter{}, err
		}
		key = strings.TrimSpace(key)
		if _, seen := fields[key]; !seen {
			order = append(order, key) // first-occurrence position, PHP-array style
		}
		fields[key] = val // last duplicate wins on value; keeps its position
	}
	return Frontmatter{Fields: fields, Order: order, Body: body}, nil
}

// stripFence reports whether s opens with a `---` fence line and, if so, returns
// the remainder after that line's terminator.
func stripFence(s string) (string, bool) {
	switch {
	case strings.HasPrefix(s, "---\n"):
		return s[len("---\n"):], true
	case strings.HasPrefix(s, "---\r\n"):
		return s[len("---\r\n"):], true
	case s == "---":
		return "", true
	default:
		return "", false
	}
}

// splitClosingFence finds the first line equal to `---` and returns the block
// before it and the body after its terminator. ok is false when there is no
// closing fence.
func splitClosingFence(s string) (block, body string, ok bool) {
	idx := 0
	for {
		nl := strings.IndexByte(s[idx:], '\n')
		var line string
		if nl < 0 {
			line = s[idx:]
		} else {
			line = s[idx : idx+nl]
		}
		if isFenceLine(line) {
			return s[:idx], remainderAfter(s, idx, nl), true
		}
		if nl < 0 {
			return "", "", false
		}
		idx += nl + 1
	}
}

// remainderAfter returns the body that follows a closing fence line whose content
// starts at lineStart and whose trailing newline is at lineStart+nl (nl<0 = EOF).
func remainderAfter(s string, lineStart, nl int) string {
	if nl < 0 {
		return ""
	}
	return s[lineStart+nl+1:]
}

// isFenceLine reports whether a line is exactly `---` (allowing a trailing \r).
func isFenceLine(line string) bool {
	return line == "---" || line == "---\r"
}

// parseScalar coerces a trimmed frontmatter value to its Go scalar type, or
// returns ErrNested for a flow collection (`[...]`/`{...}`) or an empty value
// (a bare `key:` introducing a nested block).
func parseScalar(v string) (any, error) {
	if v == "" {
		return nil, ErrNested
	}
	if v[0] == '[' || v[0] == '{' {
		return nil, ErrNested
	}
	// Quoted string: strip a single matching pair of surrounding quotes.
	if len(v) >= 2 {
		if (v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"') {
			return v[1 : len(v)-1], nil
		}
	}
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		return i, nil
	}
	if isFloatLiteral(v) {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, nil
		}
	}
	return v, nil // bare string
}

// isFloatLiteral reports whether v is a decimal float literal (-?digits.digits) —
// the only float form the frontmatter subset accepts. This keeps parsing aligned
// with the PHP tier, which uses the same shape rather than PHP's looser is_numeric.
func isFloatLiteral(v string) bool {
	i := 0
	if i < len(v) && (v[i] == '-' || v[i] == '+') {
		i++
	}
	before, after := 0, 0
	for i < len(v) && v[i] >= '0' && v[i] <= '9' {
		i++
		before++
	}
	if i >= len(v) || v[i] != '.' {
		return false
	}
	i++
	for i < len(v) && v[i] >= '0' && v[i] <= '9' {
		i++
		after++
	}
	return i == len(v) && before > 0 && after > 0
}
