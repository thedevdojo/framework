package htmlscan

import (
	"reflect"
	"testing"
)

// TestCandidates pins how a component tag name maps onto component files
// (paths relative to resources/views/components, no extension), in the order
// the renderer tries them: folders first.
func TestCandidates(t *testing.T) {
	cases := []struct {
		tag  string
		want []string
	}{
		{"Button", []string{"button"}},
		{"SectionsHero", []string{"sections/hero", "sections-hero"}},
		{"SectionsFeatureList", []string{
			"sections/feature/list",
			"sections/feature-list",
			"sections-feature/list",
			"sections-feature-list",
		}},
		// a dot is always a folder; the words inside a segment stay flexible
		{"Sections.Hero", []string{"sections/hero"}},
		{"Sections.FeatureList", []string{"sections/feature/list", "sections/feature-list"}},
		// an acronym is one word, digits stay with the word before them
		{"FAQ", []string{"faq"}},
		{"FAQSection", []string{"faq/section", "faq-section"}},
		{"Hero2", []string{"hero2"}},
		{"H1Title", []string{"h1/title", "h1-title"}},
		// an explicit dash or underscore is kept exactly
		{"Feature-List", []string{"feature-list"}},
		{"Feature_List", []string{"feature_list"}},
	}
	for _, tc := range cases {
		if got := Candidates(tc.tag); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Candidates(%q) = %v; want %v", tc.tag, got, tc.want)
		}
	}
}

// TestCandidatesAreBounded: a long name must not explode into thousands of
// file lookups. Only the first few word gaps are tried as folders.
func TestCandidatesAreBounded(t *testing.T) {
	got := Candidates("AaBbCcDdEeFfGgHhIiJjKkLl")
	if len(got) > 32 {
		t.Fatalf("Candidates produced %d paths; want at most 32", len(got))
	}
	if got[0] != "aa/bb/cc/dd/ee/ff-gg-hh-ii-jj-kk-ll" {
		t.Errorf("first candidate = %q", got[0])
	}
	if last := got[len(got)-1]; last != "aa-bb-cc-dd-ee-ff-gg-hh-ii-jj-kk-ll" {
		t.Errorf("last candidate = %q", last)
	}
}

func TestSrcBase(t *testing.T) {
	cases := []struct {
		dir, src, want string
		ok             bool
	}{
		{"layouts/", "main.html", "layouts/main", true},
		{"layouts/", "main", "layouts/main", true},
		{"layouts/", "main.blade.php", "layouts/main", true},
		{"layouts/", "marketing/main.html", "layouts/marketing/main", true},
		{"", "button.html", "button", true},
		{"", "sections/hero.html", "sections/hero", true},
		{"", "/sections/hero.html", "sections/hero", true},
		{"", " button.html ", "button", true},
		// refused: nothing to load, or a path that climbs out of components/
		{"", "", "", false},
		{"", ".html", "", false},
		{"", "../secret.html", "", false},
		{"", "a/../b.html", "", false},
		{"", "a//b.html", "", false},
		{"", "./a.html", "", false},
	}
	for _, tc := range cases {
		got, ok := srcBase(tc.dir, tc.src)
		if got != tc.want || ok != tc.ok {
			t.Errorf("srcBase(%q, %q) = %q, %v; want %q, %v", tc.dir, tc.src, got, ok, tc.want, tc.ok)
		}
	}
}
