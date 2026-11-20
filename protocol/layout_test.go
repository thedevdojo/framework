package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/vfs"
)

func runJSON(t *testing.T, input string) (Output, map[string]json.RawMessage) {
	t.Helper()
	var out bytes.Buffer
	if err := Run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got Output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(out.Bytes(), &raw)
	return got, raw
}

func TestPagesAreDiscoveredUnderSrc(t *testing.T) {
	got, raw := runJSON(t, `{"files": {
		"src/pages/index.html": "<h1>Home</h1>\n",
		"src/pages/about.blade.php": "<h1>About</h1>\n",
		"src/components/card.html": "<b>card</b>\n"
	}}`)

	if len(got.Diagnostics) > 0 {
		t.Fatalf("diagnostics: %+v", got.Diagnostics)
	}
	if got.Files["index.html"] != "<h1>Home</h1>\n" || got.Files["about.html"] != "<h1>About</h1>\n" || len(got.Files) != 2 {
		t.Fatalf("files = %q", got.Files)
	}
	// Always present, like the other keys: a host iterates it with no nil check.
	if string(raw["notices"]) != "[]" {
		t.Fatalf("notices = %s, want []", raw["notices"])
	}
}

func TestTheOldFolderIsANoticeAndNeverADiagnostic(t *testing.T) {
	got, _ := runJSON(t, `{"files": {"resources/views/pages/index.blade.php": "<h1>Home</h1>\n"}}`)

	if got.Files["index.html"] != "<h1>Home</h1>\n" {
		t.Fatalf("files = %q", got.Files)
	}
	// A host may fail a site on any diagnostic. An old folder name must not.
	if len(got.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", got.Diagnostics)
	}
	if len(got.Notices) != 1 || got.Notices[0].Code != "resources-folder" || got.Notices[0].Kind != "deprecation" {
		t.Fatalf("notices = %+v", got.Notices)
	}
}

func TestAHostCanTurnNoticesOff(t *testing.T) {
	_, raw := runJSON(t, `{"files": {"resources/views/pages/index.blade.php": "<h1>Home</h1>\n"}, "options": {"notices": false}}`)

	if string(raw["notices"]) != "[]" {
		t.Fatalf("notices = %s, want []", raw["notices"])
	}
}

func TestAURLResolvesInWhicheverFolderHoldsThePages(t *testing.T) {
	for name, pages := range map[string]string{
		"src, flat":        "src/pages/",
		"src, under views": "src/views/pages/",
		"resources":        "resources/views/pages/",
	} {
		t.Run(name, func(t *testing.T) {
			fs := vfs.Mem{
				pages + "index.html":      "x",
				pages + "about.blade.php": "x",
				pages + "blog/index.html": "x",
				pages + "404.html":        "x",
			}
			for url, want := range map[string]string{
				"/":        pages + "index.html",
				"/about":   pages + "about.blade.php",
				"/blog":    pages + "blog/index.html",
				"/missing": pages + "404.html",
			} {
				if got, ok := ResolveURL(fs, url); !ok || got != want {
					t.Errorf("ResolveURL(%q) = %q %v, want %q", url, got, ok, want)
				}
			}
		})
	}
}
