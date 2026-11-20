package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade"
)

// TestRunConvert: one JSON document in — the site's files and the syntax to
// convert to — and one out, carrying the files to write and the files they
// replace.
func TestRunConvert(t *testing.T) {
	in := `{
		"to": "studio",
		"files": {
			"resources/views/pages/index.blade.php": "<x-card :title=\"$name\" />\n",
			"resources/views/components/card.blade.php": "@props(['title' => 'T'])\n<h1>{{ $title }}</h1>\n",
			"resources/css/site.css": "body{}"
		},
		"data": {"name": "Ada"}
	}`
	var out bytes.Buffer
	if err := RunConvert(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var got ConvertOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if got.Engine != blade.Version {
		t.Errorf("engine = %q", got.Engine)
	}
	if len(got.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", got.Diagnostics)
	}
	if want := "<Card :title=\"name\" />\n"; got.Files["resources/views/pages/index.html"] != want {
		t.Errorf("page = %q; want %q", got.Files["resources/views/pages/index.html"], want)
	}
	if want := "<Props title=\"T\" />\n<h1>{{ title }}</h1>\n"; got.Files["resources/views/components/card.html"] != want {
		t.Errorf("component = %q; want %q", got.Files["resources/views/components/card.html"], want)
	}
	if len(got.Files) != 2 {
		t.Errorf("files = %v; only the two views are converted", got.Files)
	}
	want := []string{"resources/views/components/card.blade.php", "resources/views/pages/index.blade.php"}
	if strings.Join(got.Removed, ",") != strings.Join(want, ",") {
		t.Errorf("removed = %v; want %v", got.Removed, want)
	}
}

// TestRunConvertRefused: a site that cannot be converted comes back with the
// reasons and nothing to write. Every key is present, never null.
func TestRunConvertRefused(t *testing.T) {
	cases := map[string]string{
		"needs laravel":  `{"to": "html", "files": {"resources/views/pages/index.blade.php": "@auth\nx\n@endauth\n"}}`,
		"unknown syntax": `{"to": "twig", "files": {"resources/views/pages/index.blade.php": "x"}}`,
		"no syntax":      `{"files": {"resources/views/pages/index.blade.php": "x"}}`,
		"malformed json": `{"to": `,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := RunConvert(strings.NewReader(in), &out); err != nil {
				t.Fatal(err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, out.String())
			}
			if string(raw["files"]) != "{}" || string(raw["removed"]) != "[]" {
				t.Errorf("files = %s, removed = %s; want {} and []", raw["files"], raw["removed"])
			}
			var got ConvertOutput
			json.Unmarshal(out.Bytes(), &got)
			if len(got.Diagnostics) == 0 || got.Diagnostics[0].Kind != "error" {
				t.Errorf("diagnostics = %+v; want an error", got.Diagnostics)
			}
			if !strings.HasSuffix(out.String(), "\n") {
				t.Errorf("output should end in a newline")
			}
		})
	}
}

// TestRunConvertNothingToDo: a site already in the target syntax is not an
// error — there is simply nothing to write.
func TestRunConvertNothingToDo(t *testing.T) {
	var out bytes.Buffer
	if err := RunConvert(strings.NewReader(`{"to": "html", "files": {"resources/views/pages/index.html": "x"}}`), &out); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != `{"engine":"`+blade.Version+`","files":{},"removed":[],"diagnostics":[]}` {
		t.Errorf("output = %s", got)
	}
}

// TestRunConvertSyntaxNames: the Studio syntax is asked for as "studio". "html"
// is its older name and stays accepted, in any case, so a host written against
// it keeps working. An unknown name is refused, naming the two that exist.
func TestRunConvertSyntaxNames(t *testing.T) {
	run := func(to string) ConvertOutput {
		t.Helper()
		in := `{"to": "` + to + `", "files": {"resources/views/pages/index.blade.php": "@if ($on) yes @endif\n"}, "data": {"on": true}}`
		var out bytes.Buffer
		if err := RunConvert(strings.NewReader(in), &out); err != nil {
			t.Fatal(err)
		}
		var got ConvertOutput
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, out.String())
		}
		return got
	}
	for _, to := range []string{"studio", "Studio", "html", "HTML"} {
		got := run(to)
		if len(got.Diagnostics) != 0 {
			t.Errorf("to %q: diagnostics = %+v", to, got.Diagnostics)
		}
		if want := "<If on> yes </If>\n"; got.Files["resources/views/pages/index.html"] != want {
			t.Errorf("to %q: page = %q; want %q", to, got.Files["resources/views/pages/index.html"], want)
		}
	}
	got := run("twig")
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != `unknown syntax "twig": convert to "blade" or "studio"` {
		t.Errorf("unknown syntax: diagnostics = %+v", got.Diagnostics)
	}
}
