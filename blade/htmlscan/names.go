package htmlscan

import "strings"

// maxFolderGaps bounds how many word gaps of a component tag are tried as a
// folder separator. Each free gap doubles the candidate list, so five gaps cap
// a tag at 32 paths; any gap past the fifth is always a dash.
const maxFolderGaps = 5

// Candidates maps a component tag name to the component files it may name, as
// paths relative to resources/views/components without an extension, in the
// order the renderer tries them.
//
// The tag is split into words at its capital letters. Between two words the
// file name may have either a folder or a dash, so <SectionsHero> finds
// sections/hero or sections-hero — folders are tried first, leftmost gap
// first. A dot in the tag is always a folder (<Sections.FeatureList> is
// sections/feature/list or sections/feature-list), and a dash or underscore
// written in the tag is kept exactly.
func Candidates(tag string) []string {
	var words []string
	var fixed []bool // fixed[i]: the gap after words[i] is a dot, so always a folder
	for _, seg := range strings.Split(tag, ".") {
		for wi, w := range splitWords(seg) {
			if len(words) > 0 {
				fixed = append(fixed, wi == 0)
			}
			words = append(words, strings.ToLower(w))
		}
	}
	if len(words) == 0 {
		return nil
	}

	free := 0
	for _, f := range fixed {
		if !f {
			free++
		}
	}
	enumerated := free
	if enumerated > maxFolderGaps {
		enumerated = maxFolderGaps
	}

	out := make([]string, 0, 1<<enumerated)
	for mask := 0; mask < 1<<enumerated; mask++ {
		var b strings.Builder
		k := 0 // index among the free gaps
		for i, w := range words {
			b.WriteString(w)
			if i == len(words)-1 {
				break
			}
			sep := byte('/')
			if !fixed[i] {
				if k >= enumerated || mask>>(enumerated-1-k)&1 == 1 {
					sep = '-'
				}
				k++
			}
			b.WriteByte(sep)
		}
		out = append(out, b.String())
	}
	return out
}

// splitWords splits one tag segment at its capital letters: a capital starts a
// word when it follows a lowercase letter or digit (SectionsHero, H1Title), or
// when it ends a run of capitals and a lowercase letter follows (FAQSection →
// FAQ, Section). Dashes and underscores stay inside their word.
func splitWords(seg string) []string {
	var words []string
	start := 0
	for i := 1; i < len(seg); i++ {
		if !isUpper(seg[i]) {
			continue
		}
		prev := seg[i-1]
		boundary := isLower(prev) || isDigit(prev) ||
			(isUpper(prev) && i+1 < len(seg) && isLower(seg[i+1]))
		if boundary {
			words = append(words, seg[start:i])
			start = i
		}
	}
	if start < len(seg) {
		words = append(words, seg[start:])
	}
	return words
}

// dotted turns a component base path into the dotted component name Blade
// uses for it (sections/hero → sections.hero), the name diagnostics and the
// missing-component marker carry.
func dotted(base string) string {
	return strings.ReplaceAll(base, "/", ".")
}

// srcBase resolves the src of a <Layout> or <Include> to a component base
// path: dir (the folder under resources/views/components the tag looks in)
// plus src without its extension. It refuses an empty src and any src with an
// empty, "." or ".." segment, so a tag can never name a file outside the
// components folder.
func srcBase(dir, src string) (string, bool) {
	s := strings.TrimSpace(src)
	s = strings.TrimPrefix(s, "/")
	switch {
	case strings.HasSuffix(s, ".blade.php"):
		s = strings.TrimSuffix(s, ".blade.php")
	case strings.HasSuffix(s, ".html"):
		s = strings.TrimSuffix(s, ".html")
	}
	if s == "" {
		return "", false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return dir + s, true
}
