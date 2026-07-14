# Pocketknife v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Pocketknife v1 engine per `docs/specs/2026-07-14-pocketknife-design.md`: a Go static renderer for a closed Blade subset, the `devdojo/pocketknife` Laravel package, a dual-oracle conformance suite, the compile/lint CLI, and 2–3 hand-converted templates.

**Architecture:** AST-based (never regex-over-documents), byte-faithful engine in `blade/` (scanner → parser → renderer + expression evaluator), with `vfs/` abstracting file access and `protocol/` exposing JSON stdin/stdout. `laravel/` is the Composer package making the same files render in real Laravel; `oracle/` is a Laravel skeleton that byte-diffs every fixture against real Blade. **The oracle is built early (Task 2)** so all subsequent semantics (escaping, stringification, newline handling) are verified empirically, not assumed.

**Tech Stack:** Go 1.22+ (stdlib-only core; `goldmark` allowed in `content/` only), PHP 8.3 + Laravel 11 (oracle), Composer path packages.

## Global Constraints (from the spec — every task inherits these)

- **Never `eval`, never panic on user input.** All engine errors become diagnostics.
- **Byte-faithful:** unrecognized input passes through verbatim; no HTML normalization.
- **Core stdlib-only:** `blade/*`, `vfs/`, `protocol/` import only the Go standard library (and each other). `goldmark` is permitted only in `content/`.
- **Every node/token/diagnostic carries file/line/col** (1-based line and col).
- **Fixture-first:** a behavior without a fixture doesn't exist. Fixture cases must pass BOTH renderers byte-identically unless `case.json` sets `"oracle": false`.
- **Diagnostics** use `kind: "error"` or `kind: "app-tier"` exactly (§3.7 of spec).
- **File extension:** views are `*.blade.html`. Structure: `resources/views/pages`, `resources/views/components`, `resources/data/{site.json,collections,content}`, `public/`.
- Repo: `~/devdojo/pocketknife`, module `github.com/thedevdojo/pocketknife`, branch `main`. Commit after every green task step-pair; `gofmt`/`go vet` clean before each commit.
- Templates work happens in `~/devdojo/templates` on branch `blade-dev`.

## File Structure (locked by this plan)

```
pocketknife/
├── go.mod                        # module github.com/thedevdojo/pocketknife; go 1.22
├── Makefile                      # test / fuzz / conformance / lint targets
├── .github/workflows/ci.yml     # Task 13
├── vfs/vfs.go                    # FS interface + Mem + Dir impls          (Task 1)
├── fixtures/                     # conformance cases + harness            (Task 1)
│   ├── harness_test.go
│   └── <case>/{input/**,expected/**,case.json}
├── oracle/                       # Laravel skeleton + render command       (Task 2)
│   ├── composer.json  (vendor/ gitignored)
│   └── app/Console/Commands/RenderFixture.php
├── laravel/                      # devdojo/pocketknife composer package    (Tasks 2,9,10)
│   ├── composer.json
│   ├── config/pocketknife.php
│   └── src/{PocketknifeServiceProvider.php,SiteData.php,StaticVite.php}
├── blade/
│   ├── scanner/scanner.go        # source → tokens                        (Task 3)
│   ├── ast/ast.go                # node types                             (Task 4)
│   ├── parser/parser.go          # tokens → AST, depth-matched            (Task 4)
│   ├── expr/expr.go              # evaluator                              (Task 5)
│   └── render/
│       ├── render.go             # Compile entry, page walk               (Task 6)
│       ├── echo.go               # Escape, Stringify                      (Task 6)
│       ├── control.go            # if/foreach/$loop/break                 (Task 7)
│       ├── component.go          # x-tags, @props, slots                  (Task 8)
│       └── diagnostics.go        # classification + undefined-var         (Task 11)
├── content/markdown.go           # goldmark pipeline                      (Task 9)
├── protocol/protocol.go          # JSON stdin/stdout                      (Task 12)
└── cmd/pocketknife/main.go       # compile | lint | version               (Task 12)
```

---

### Task 1: Repo scaffold, vfs, fixture harness

**Files:**
- Create: `go.mod`, `.gitignore`, `Makefile`, `vfs/vfs.go`, `vfs/vfs_test.go`, `fixtures/harness_test.go`, `fixtures/passthrough/input/resources/views/pages/index.blade.html`, `fixtures/passthrough/expected/index.html`, `fixtures/passthrough/case.json`

**Interfaces (later tasks rely on exactly these):**
- `vfs.FS` interface: `Read(path string) ([]byte, bool)`; `List(prefix string) []string` (sorted, forward-slash paths relative to site root)
- `vfs.Mem map[string]string` and `vfs.Dir string` both implement `FS`
- Fixture `case.json` schema: `{"entries": ["resources/views/pages/index.blade.html"], "data": {}, "oracle": true, "diagnostics": [{"kind":"...","construct":"...","file":"...","line":1}]}` — all keys optional except `entries`; `oracle` defaults to `true`
- The harness calls `render.Compile(fs, entries, render.Options{})` — Task 6 provides it; until then the harness build-tags itself out (see Step 4)

- [ ] **Step 1: Scaffold module**

```bash
cd ~/devdojo/pocketknife
go mod init github.com/thedevdojo/pocketknife
printf 'oracle/vendor/\noracle/storage/\n.DS_Store\n' > .gitignore
```

`Makefile`:
```makefile
test:
	go test ./...
fuzz:
	go test ./blade/scanner -fuzz=FuzzScan -fuzztime=30s
	go test ./blade/parser -fuzz=FuzzParse -fuzztime=30s
conformance:
	cd oracle && php artisan pocketknife:conformance ../fixtures
```

- [ ] **Step 2: Write failing vfs test** (`vfs/vfs_test.go`)

```go
package vfs

import "testing"

func TestMemReadAndList(t *testing.T) {
	fs := Mem{
		"resources/views/pages/index.blade.html": "<h1>Hi</h1>",
		"resources/views/pages/about.blade.html": "<h1>About</h1>",
		"resources/data/site.json":               `{"name":"X"}`,
	}
	b, ok := fs.Read("resources/views/pages/index.blade.html")
	if !ok || string(b) != "<h1>Hi</h1>" {
		t.Fatalf("Read = %q, %v", b, ok)
	}
	if _, ok := fs.Read("missing"); ok {
		t.Fatal("missing path must return ok=false")
	}
	got := fs.List("resources/views/pages/")
	want := []string{
		"resources/views/pages/about.blade.html",
		"resources/views/pages/index.blade.html",
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List = %v, want %v", got, want)
	}
}
```

Run: `go test ./vfs/` → FAIL (undefined: Mem)

- [ ] **Step 3: Implement `vfs/vfs.go`, make it pass**

```go
// Package vfs abstracts site-source file access: disk for the CLI and
// oracle, in-memory for the JSON protocol. Paths are forward-slash,
// relative to the site root.
package vfs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FS interface {
	Read(path string) ([]byte, bool)
	List(prefix string) []string // sorted paths under prefix
}

type Mem map[string]string

func (m Mem) Read(path string) ([]byte, bool) {
	s, ok := m[path]
	return []byte(s), ok
}

func (m Mem) List(prefix string) []string {
	var out []string
	for p := range m {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

type Dir string

func (d Dir) Read(path string) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(string(d), filepath.FromSlash(path)))
	if err != nil {
		return nil, false
	}
	return b, true
}

func (d Dir) List(prefix string) []string {
	root := filepath.Join(string(d), filepath.FromSlash(prefix))
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(string(d), p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}
```

Run: `go test ./vfs/` → PASS

- [ ] **Step 4: Fixture harness + first fixture**

`fixtures/passthrough/input/resources/views/pages/index.blade.html` (plain HTML — must pass through byte-identical, including the entity and the weird spacing):

```html
<!DOCTYPE html>
<html>
<head><title>Passthrough &amp; Co.</title></head>
<body>
  <h1  class="x">No   Blade here</h1>
</body>
</html>
```

`fixtures/passthrough/expected/index.html`: identical bytes to the input file.
`fixtures/passthrough/case.json`: `{"entries": ["resources/views/pages/index.blade.html"]}`

`fixtures/harness_test.go` — note the build tag: it compiles only once Task 6 lands `render.Compile`; remove the tag in Task 6, Step 1.

```go
//go:build renderready

package fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/vfs"
)

type caseSpec struct {
	Entries     []string                 `json:"entries"`
	Data        map[string]any           `json:"data"`
	Oracle      *bool                    `json:"oracle"`
	Diagnostics []map[string]any         `json:"diagnostics"`
}

func TestFixtures(t *testing.T) {
	dirs, _ := os.ReadDir(".")
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(name, "case.json"))
			if err != nil {
				t.Fatalf("case.json: %v", err)
			}
			var spec caseSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatalf("case.json: %v", err)
			}
			res := render.Compile(vfs.Dir(filepath.Join(name, "input")), spec.Entries, render.Options{Data: spec.Data})
			// Compare every expected/** file byte-for-byte.
			expRoot := filepath.Join(name, "expected")
			filepath.Walk(expRoot, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(expRoot, p)
				rel = filepath.ToSlash(rel)
				want, _ := os.ReadFile(p)
				got, ok := res.Files[rel]
				if !ok {
					t.Errorf("missing output %s (have %v)", rel, keys(res.Files))
					return nil
				}
				if got != string(want) {
					t.Errorf("%s mismatch:\n--- want ---\n%s\n--- got ---\n%s", rel, want, got)
				}
				return nil
			})
			// Expected diagnostics: match on kind+construct+file+line.
			for _, wd := range spec.Diagnostics {
				if !hasDiag(res.Diagnostics, wd) {
					t.Errorf("expected diagnostic %v not found in %v", wd, res.Diagnostics)
				}
			}
		})
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasDiag(ds []render.Diagnostic, want map[string]any) bool {
	for _, d := range ds {
		if string(d.Kind) == want["kind"] &&
			(want["construct"] == nil || d.Construct == want["construct"]) &&
			(want["file"] == nil || d.File == want["file"]) &&
			(want["line"] == nil || float64(d.Line) == want["line"]) {
			return true
		}
	}
	return false
}

var _ = strings.TrimSpace // keep imports honest until fleshed out
```

Run: `go test ./...` → PASS (harness excluded by build tag; vfs green)

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: scaffold module, vfs, fixture harness + passthrough fixture"
```

---

### Task 2: Oracle skeleton + minimal `laravel/` package

The oracle exists from here on so every later fixture is verified against real Blade **as it's written** — semantics questions (newline eating, float formatting, escaping) get empirical answers, never guesses.

**Files:**
- Create: `laravel/composer.json`, `laravel/config/pocketknife.php`, `laravel/src/PocketknifeServiceProvider.php`
- Create: `oracle/` (Laravel 11 skeleton via composer, committed without `vendor/`), `oracle/app/Console/Commands/PocketknifeConformance.php`

**Interfaces:**
- Consumes: fixture layout + `case.json` from Task 1
- Produces: `php artisan pocketknife:conformance <fixtures-dir>` — for each case dir with `"oracle" != false`: points the view finder at `input/resources/views`, renders each entry through real Blade, byte-diffs against `expected/**`, exits non-zero listing mismatched files. Also `pocketknife:render <input-dir> <entry>` printing rendered output (the manual-probe tool). The provider registers `View::addExtension('blade.html', 'blade')` — the package's first real behavior.

- [ ] **Step 1: Package skeleton**

`laravel/composer.json`:
```json
{
    "name": "devdojo/pocketknife",
    "description": "Run Pocketknife (portable Blade) sites in a real Laravel application",
    "type": "library",
    "license": "MIT",
    "require": {
        "php": "^8.2",
        "illuminate/support": "^11.0|^12.0|^13.0",
        "illuminate/view": "^11.0|^12.0|^13.0"
    },
    "autoload": { "psr-4": { "Devdojo\\Pocketknife\\": "src/" } },
    "extra": { "laravel": { "providers": ["Devdojo\\Pocketknife\\PocketknifeServiceProvider"] } },
    "minimum-stability": "dev",
    "prefer-stable": true
}
```

`laravel/config/pocketknife.php`:
```php
<?php

return [
    // Absolute path to the site root (the directory holding resources/ and public/).
    'site_root' => env('POCKETKNIFE_SITE_ROOT'),

    // When true, @vite renders the static projection (Play CDN + inlined CSS)
    // instead of requiring a Vite build. Task 10 implements this.
    'static_projection' => env('POCKETKNIFE_STATIC_PROJECTION', true),
];
```

`laravel/src/PocketknifeServiceProvider.php`:
```php
<?php

namespace Devdojo\Pocketknife;

use Illuminate\Support\Facades\Blade;
use Illuminate\Support\Facades\View;
use Illuminate\Support\ServiceProvider;

class PocketknifeServiceProvider extends ServiceProvider
{
    public function register(): void
    {
        $this->mergeConfigFrom(__DIR__.'/../config/pocketknife.php', 'pocketknife');
    }

    public function boot(): void
    {
        // The one line that makes *.blade.html real Blade.
        View::addExtension('blade.html', 'blade');

        if ($root = config('pocketknife.site_root')) {
            View::addLocation($root.'/resources/views');
            Blade::anonymousComponentPath($root.'/resources/views/components');
        }
    }
}
```

- [ ] **Step 2: Oracle skeleton**

```bash
cd ~/devdojo/pocketknife
composer create-project laravel/laravel:^11.0 oracle --no-interaction
cd oracle
composer config repositories.pocketknife path ../laravel
composer require devdojo/pocketknife:@dev
```

Commit `oracle/` WITHOUT `vendor/` or `storage/` (already gitignored). Trim nothing else — a stock skeleton is fine; it's a test rig.

- [ ] **Step 3: The conformance command**

`oracle/app/Console/Commands/PocketknifeConformance.php`:
```php
<?php

namespace App\Console\Commands;

use Illuminate\Console\Command;
use Illuminate\Support\Facades\Blade;
use Illuminate\Support\Facades\File;
use Illuminate\Support\Facades\View;

class PocketknifeConformance extends Command
{
    protected $signature = 'pocketknife:conformance {fixtures} {--case= : run a single case}';
    protected $description = 'Render fixture entries through real Blade and byte-diff against expected/';

    public function handle(): int
    {
        $failures = 0;
        foreach (File::directories($this->argument('fixtures')) as $dir) {
            $name = basename($dir);
            if ($this->option('case') && $this->option('case') !== $name) {
                continue;
            }
            $case = json_decode(File::get("$dir/case.json"), true);
            if (($case['oracle'] ?? true) === false) {
                continue;
            }
            foreach ($case['entries'] as $entry) {
                // entry: resources/views/pages/index.blade.html → view name pages.index, output index.html
                $view = str_replace('/', '.', preg_replace('#^resources/views/(.+)\.blade\.html$#', '$1', $entry));
                $out = preg_replace('#^pages\.#', '', $view);
                $outFile = str_replace('.', '/', $out).'.html';

                config(['pocketknife.site_root' => "$dir/input"]);
                View::flushFinderViews();
                View::getFinder()->setPaths(["$dir/input/resources/views"]);
                Blade::anonymousComponentPath("$dir/input/resources/views/components");

                $rendered = View::make($view, $case['data'] ?? [])->render();
                $expected = File::get("$dir/expected/$outFile");
                if ($rendered !== $expected) {
                    $failures++;
                    $this->error("FAIL $name :: $outFile");
                    $this->line($this->firstDiff($expected, $rendered));
                } else {
                    $this->info("ok   $name :: $outFile");
                }
            }
        }
        return $failures === 0 ? self::SUCCESS : self::FAILURE;
    }

    private function firstDiff(string $want, string $got): string
    {
        $len = min(strlen($want), strlen($got));
        for ($i = 0; $i < $len; $i++) {
            if ($want[$i] !== $got[$i]) {
                return sprintf('first diff at byte %d: want %s got %s',
                    $i, json_encode(substr($want, $i, 40)), json_encode(substr($got, $i, 40)));
            }
        }
        return sprintf('length differs: want %d got %d', strlen($want), strlen($got));
    }
}
```

Also create `pocketknife:render {input} {entry}` as a sibling command with the same view-resolution block that just prints the rendered bytes — the manual probe used throughout later tasks.

- [ ] **Step 4: Verify against the passthrough fixture**

Run: `cd oracle && php artisan pocketknife:conformance ../fixtures`
Expected: `ok passthrough :: index.html`, exit 0. (Data bindings like `$site` come in Task 9 — until then fixtures needing them don't exist.)

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: laravel package (blade.html extension) + oracle conformance runner"
```

---

### Task 3: Scanner (`blade/scanner`)

**Files:**
- Create: `blade/scanner/scanner.go`, `blade/scanner/scanner_test.go`, `blade/scanner/fuzz_test.go`

**Interfaces (Produces — parser and renderer depend on exactly these):**

```go
type Kind int
const (
	Text Kind = iota // opaque bytes, emitted verbatim
	Echo             // {{ expr }}
	RawEcho          // {!! expr !!}
	Comment          // {{-- ... --}}
	Directive        // @if/@elseif/@else/@endif/@foreach/@endforeach/@break/@continue/@props/@vite — and ANY other @word (classified later)
	TagOpen          // <x-name ...> or <x-name .../>
	TagClose         // </x-name>
	SlotOpen         // <x-slot:name>
	SlotClose        // </x-slot>
)
type Pos struct{ File string; Line, Col int } // 1-based
type Attr struct{ Name, Value string; Bound bool; Pos Pos } // Bound: :name="expr"
type Token struct {
	Kind      Kind
	Text      string // Text: raw bytes; Echo/RawEcho: inner expr (trimmed); Directive: raw parenthesized args without outer parens ("" if none)
	Name      string // Directive: lowercase name; TagOpen/TagClose: component name w/ dots ("sections.hero"); SlotOpen: slot name
	Attrs     []Attr
	SelfClose bool
	Pos       Pos
}
func Scan(file string, src []byte) []Token
```

Scanner rules (each is a test case in Step 1):
1. `@@if` and `@{{` are Blade escapes → emit as Text `@if` / literal `{{` (real Blade behavior — verify with `pocketknife:render`).
2. Directive args: balanced-parens capture — `@if ($a == 'x)')` captures `$a == 'x)'` because quotes are lexed. Unterminated → the `@word` becomes Text (byte-faithful, never panic).
3. `{{` without matching `}}` before EOF → Text. Same for `{!!` and `{{--`.
4. `<x-` tags: attribute lexing handles `'` and `"` quotes, `>` inside quoted values, boolean attributes (no value), `:bound="expr"` attrs, self-close `/>`.
5. Anything not starting a recognized construct is Text; consecutive text coalesces.
6. Every token records Pos at its first byte.

- [ ] **Step 1: Write failing golden-token tests** — table-driven; include at minimum:

```go
package scanner

import "testing"

func TestScanGolden(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []Token // compare Kind/Name/Text/SelfClose and Pos.Line/Col
	}{
		{"plain", `<p>hi</p>`, []Token{{Kind: Text, Text: "<p>hi</p>", Pos: at(1, 1)}}},
		{"echo", `a {{ $x }} b`, []Token{
			{Kind: Text, Text: "a ", Pos: at(1, 1)},
			{Kind: Echo, Text: "$x", Pos: at(1, 3)},
			{Kind: Text, Text: " b", Pos: at(1, 11)},
		}},
		{"raw", `{!! $html !!}`, []Token{{Kind: RawEcho, Text: "$html", Pos: at(1, 1)}}},
		{"comment", `x{{-- gone --}}y`, []Token{
			{Kind: Text, Text: "x", Pos: at(1, 1)},
			{Kind: Comment, Pos: at(1, 2)},
			{Kind: Text, Text: "y", Pos: at(1, 16)},
		}},
		{"directive_args_quotes", `@if ($a == 'x)')`, []Token{
			{Kind: Directive, Name: "if", Text: `$a == 'x)'`, Pos: at(1, 1)},
		}},
		{"directive_multiline", "@foreach (\n  $items as $item\n)", []Token{
			{Kind: Directive, Name: "foreach", Text: "$items as $item", Pos: at(1, 1)},
		}},
		{"escape_at", `@@if`, []Token{{Kind: Text, Text: "@if", Pos: at(1, 1)}}},
		{"escape_braces", `@{{ raw }}`, []Token{{Kind: Text, Text: "{{ raw }}", Pos: at(1, 1)}}},
		{"unterminated_echo", `{{ $x`, []Token{{Kind: Text, Text: "{{ $x", Pos: at(1, 1)}}},
		{"xtag", `<x-sections.hero heading="Big &amp; bold" :plans="$plans" wide/>`, []Token{
			{Kind: TagOpen, Name: "sections.hero", SelfClose: true, Pos: at(1, 1), Attrs: []Attr{
				{Name: "heading", Value: "Big &amp; bold"},
				{Name: "plans", Value: "$plans", Bound: true},
				{Name: "wide", Value: ""},
			}},
		}},
		{"xtag_gt_in_attr", `<x-btn label="a > b">go</x-btn>`, []Token{
			{Kind: TagOpen, Name: "btn", Attrs: []Attr{{Name: "label", Value: "a > b"}}, Pos: at(1, 1)},
			{Kind: Text, Text: "go", Pos: at(1, 22)},
			{Kind: TagClose, Name: "btn", Pos: at(1, 24)},
		}},
		{"slot", `<x-slot:title>T</x-slot>`, []Token{
			{Kind: SlotOpen, Name: "title", Pos: at(1, 1)},
			{Kind: Text, Text: "T", Pos: at(1, 15)},
			{Kind: SlotClose, Pos: at(1, 16)},
		}},
		{"newlines_pos", "line1\n{{ $x }}", []Token{
			{Kind: Text, Text: "line1\n", Pos: at(1, 1)},
			{Kind: Echo, Text: "$x", Pos: at(2, 1)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertTokens(t, Scan("t.blade.html", []byte(c.src)), c.want) })
	}
}
```

(`at(l, c int) Pos` and `assertTokens` are small test helpers in the same file — compare Kind, Name, Text, SelfClose, Attrs, Line, Col; print a labeled diff on mismatch.)

Run: `go test ./blade/scanner/` → FAIL (undefined: Scan)

- [ ] **Step 2: Implement the scanner**

Single forward pass over `src` tracking line/col. Structure:

```go
func Scan(file string, src []byte) []Token {
	s := &scanState{file: file, src: src, line: 1, col: 1}
	for !s.eof() {
		switch {
		case s.has("@@"):
			s.textEscape(2, "@") // emit "@" + following word as Text
		case s.has("@{{"):
			s.textEscape(1, "")  // drop the @, emit "{{..." as Text
		case s.has("{{--"):
			s.comment()
		case s.has("{!!"):
			s.echo("{!!", "!!}", RawEcho)
		case s.has("{{"):
			s.echo("{{", "}}", Echo)
		case s.has("</x-slot"):
			s.slotClose()
		case s.has("<x-slot:"):
			s.slotOpen()
		case s.has("</x-"):
			s.tagClose()
		case s.has("<x-"):
			s.tagOpen()
		case s.atDirective(): // '@' + ascii letter
			s.directive()
		default:
			s.text() // consume until the next possible construct start ('{', '<', '@')
		}
	}
	return s.tokens
}
```

Key details: `directive()` reads the word, then — if the next non-space byte is `(` — captures balanced parens while honoring single/double-quoted strings; on unterminated anything, rewind and emit as Text. `tagOpen()` lexes attributes: name (`[a-zA-Z0-9_:-]+` with a leading `:` meaning Bound), optional `="…"`/`='…'` value; malformed tag (EOF before `>`) → Text. Text tokens coalesce.

Run: `go test ./blade/scanner/` → PASS

- [ ] **Step 3: Fuzz target** (`blade/scanner/fuzz_test.go`)

```go
package scanner

import "testing"

func FuzzScan(f *testing.F) {
	for _, seed := range []string{"{{ $x }}", "@if($a)x@endif", "<x-a b='c'>", "@{{", "{{--", "@foreach(", "</x-slot>"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks := Scan("f.blade.html", []byte(src)) // must not panic
		// Reconstruction invariant: total consumed bytes == len(src) is NOT
		// guaranteed (escapes rewrite), but token positions must be ordered.
		last := Pos{Line: 1, Col: 0}
		for _, tk := range toks {
			if tk.Pos.Line < last.Line || (tk.Pos.Line == last.Line && tk.Pos.Col <= last.Col) {
				t.Fatalf("positions not monotonic: %+v after %+v", tk.Pos, last)
			}
			last = tk.Pos
		}
	})
}
```

Run: `go test ./blade/scanner -fuzz=FuzzScan -fuzztime=30s` → no crashes

- [ ] **Step 4: Commit**

```bash
git add blade/scanner && git commit -m "feat(scanner): tokenize the Blade subset with positions, escapes, fuzz"
```

---

### Task 4: AST + parser (`blade/ast`, `blade/parser`)

**Files:**
- Create: `blade/ast/ast.go`, `blade/parser/parser.go`, `blade/parser/parser_test.go`, `blade/parser/fuzz_test.go`

**Interfaces (Produces):**

```go
// blade/ast
type Node interface{ Pos() scanner.Pos }
type Text struct{ Bytes string; P scanner.Pos }
type Echo struct{ Expr string; Raw bool; P scanner.Pos }
type If struct{ Branches []Branch; Else []Node; P scanner.Pos } // Branches[0]=@if, rest=@elseif
type Branch struct{ Cond string; Body []Node; P scanner.Pos }
type Foreach struct{ ItemsExpr, KeyVar, ValVar string; Body []Node; P scanner.Pos } // KeyVar "" when absent
type LoopJump struct{ Cond string; Continue bool; P scanner.Pos } // @break / @continue; Cond "" = unconditional
type Component struct {
	Name      string // "sections.hero"
	Attrs     []scanner.Attr
	Body      []Node            // default slot content
	Slots     map[string][]Node // named slots
	SelfClose bool
	P         scanner.Pos
}
type Props struct{ Raw string; P scanner.Pos } // @props(...) literal; render evaluates it
type Vite struct{ Raw string; P scanner.Pos }  // @vite([...]) args
type Unknown struct{ Name, Raw string; P scanner.Pos } // any other directive — renderer classifies app-tier

// blade/parser
type Diag struct{ File string; Line, Col int; Message string } // parse errors (kind "error")
func Parse(file string, tokens []scanner.Token) ([]ast.Node, []Diag)
```

Parser rules (each a test):
1. Depth-matched pairs: `@if…@endif`, `@foreach…@endforeach`, `<x-a>…</x-a>` — **nested same-kind constructs must nest correctly** (the regex failure the AST exists to kill).
2. `@foreach` args parsed as `EXPR as $val` or `EXPR as $key => $val`; anything else → Diag + node dropped, body still parsed.
3. Named slots (`<x-slot:name>`) legal only directly inside a component body → else Diag.
4. Unclosed pair at EOF → Diag pointing at the OPENING token's Pos; body kept as if closed at EOF (render still produces output).
5. Stray `@endif`/`@endforeach`/`</x-a>` → Diag, token dropped.
6. `@props` legal only as the first non-Text node of a component file — enforced at render (parser just builds the node).

- [ ] **Step 1: Write failing tree-shape tests** — table-driven; the critical cases:

```go
func TestNestedForeach(t *testing.T) {
	src := `@foreach ($rows as $row)@foreach ($row->cells as $cell){{ $cell }}@endforeach|@endforeach`
	nodes, diags := parseSrc(t, src)
	requireNoDiags(t, diags)
	outer := nodes[0].(*ast.Foreach)
	if outer.ValVar != "$row" || len(outer.Body) != 2 {
		t.Fatalf("outer = %+v", outer)
	}
	inner := outer.Body[0].(*ast.Foreach)
	if inner.ItemsExpr != "$row->cells" || inner.Body[0].(*ast.Echo).Expr != "$cell" {
		t.Fatalf("inner = %+v", inner)
	}
	if outer.Body[1].(*ast.Text).Bytes != "|" {
		t.Fatal("text after inner close must belong to outer body")
	}
}

func TestIfElseifElse(t *testing.T) {
	src := `@if ($a)A@elseif ($b)B@else C@endif`
	nodes, _ := parseSrc(t, src)
	n := nodes[0].(*ast.If)
	if len(n.Branches) != 2 || n.Branches[0].Cond != "$a" || n.Branches[1].Cond != "$b" {
		t.Fatalf("branches = %+v", n.Branches)
	}
	if n.Else[0].(*ast.Text).Bytes != " C" {
		t.Fatalf("else = %+v", n.Else)
	}
}

func TestComponentSlots(t *testing.T) {
	src := `<x-layouts.main title="Home"><x-slot:head><meta></x-slot><p>body</p></x-layouts.main>`
	nodes, _ := parseSrc(t, src)
	c := nodes[0].(*ast.Component)
	if c.Name != "layouts.main" || c.Slots["head"] == nil || len(c.Body) != 1 {
		t.Fatalf("component = %+v", c)
	}
}

func TestUnclosedIfDiag(t *testing.T) {
	_, diags := parseSrc(t, "line1\n@if ($a)\nnever closed")
	if len(diags) != 1 || diags[0].Line != 2 {
		t.Fatalf("want 1 diag at line 2, got %+v", diags)
	}
}
```

Run: `go test ./blade/parser/` → FAIL

- [ ] **Step 2: Implement** — recursive descent over the token slice with an explicit stack; `@foreach` args split by the ` as ` keyword (rightmost, outside quotes), then `$key => $val` on `=>`. Keep a `parseBody(stop func(tok) bool)` helper shared by if-branches, loops, components, and slots.

Run: `go test ./blade/parser/` → PASS

- [ ] **Step 3: Fuzz** (`FuzzParse`: `Parse("f", Scan("f", src))` must never panic; diags may be anything). Run 30s → clean.

- [ ] **Step 4: Commit** — `git commit -m "feat(parser): depth-matched AST with recoverable diagnostics"`

---

### Task 5: Expression evaluator (`blade/expr`)

**Files:**
- Create: `blade/expr/expr.go`, `blade/expr/expr_test.go`

**Interfaces (Produces):**

```go
type Value any // nil | bool | int64 | float64 | string | []Value | map[string]Value
type Scope interface{ Lookup(name string) (Value, bool) } // name without '$'
// Eval returns ErrUndefinedVar (wrapped, with the var name) when a variable
// is missing AND not protected by ??. Any other malformed input → *SyntaxError.
func Eval(src string, scope Scope) (Value, error)
// Static analysis for Task 11: variables the expression needs (excluding ??-protected LHS).
func RequiredVars(src string) ([]string, error)
func Truthy(v Value) bool
func LooseEq(a, b Value) bool
```

Grammar (spec §3.4): `$name`, `->prop`, `['key']`/`[0]`, `??`, `== != < > <= >=` (non-associative), `&& || !`, parens, literals (`'s'`, `"s"`, ints, floats, `true/false/null`). Precedence: `!` > comparisons > `&&` > `||` > `??` (lowest, right-assoc — matches PHP). Function calls/`?->`/`.` concat → `*SyntaxError` with an `AppTier bool` field set true (the renderer maps it to an app-tier diagnostic; garbage stays kind error).

- [ ] **Step 1: Write failing truth tables** — cover at minimum:

```go
// scope: plans = []Value{map{"name":"Pro","price":int64(9)}}, site = map{"name":"X"}, zero = int64(0)
{`$site->name`, "X"}, {`$plans[0]['price']`, int64(9)},
{`$missing ?? 'd'`, "d"}, {`$site->nope ?? 'd'`, "d"},
{`$zero == '0'`, true},   // PHP loose equality
{`'1' == 1`, true}, {`'abc' == 0`, false}, // PHP 8 semantics
{`1 < 2`, true}, {`'10' < '9'`, true},     // both numeric-ish → numeric compare
{`'a' < 'b'`, true},                        // byte-order string compare
{`!$zero`, true}, {`$zero || true`, true}, {`null ?? false ?? 'x'`, false},
// errors:
{`$missing`, ErrUndefinedVar}, {`1 < 2 < 3`, SyntaxError}, {`count($x)`, SyntaxError{AppTier: true}},
```

Truthy table: `false,0,0.0,"","0",null,[]` falsy; `"false","00",-1` truthy.

Run: `go test ./blade/expr/` → FAIL

- [ ] **Step 2: Implement** — hand-written tokenizer + recursive descent (port the classic evaluator's proven semantics; PHP-flavored syntax). `??` catches ErrUndefinedVar and nil from its LHS only. Property/index access on nil or missing key yields nil (then `??` or comparison handles it) — matching PHP's null-propagation-with-notice behavior, which Blade templates rely on via `??`. NOTE: verify that exact nuance against the oracle in Task 6 Step 4 (`{{ $site->nope ?? 'd' }}` fixture) — if real Blade throws instead, missing-property becomes ErrUndefinedVar too.

Run: `go test ./blade/expr/` → PASS

- [ ] **Step 3: Commit** — `git commit -m "feat(expr): PHP-semantics evaluator with ??-aware undefined handling"`

---

### Task 6: Render core — echoes, escaping, stringification, PHP newline rule (`blade/render`)

**Files:**
- Create: `blade/render/render.go`, `blade/render/echo.go`, `blade/render/render_test.go`
- Modify: `fixtures/harness_test.go` (remove the `//go:build renderready` tag)
- Create fixtures: `echo-escaping/`, `stringify/`, `newline-eating/`, `comments/`

**Interfaces (Produces — the harness, protocol, and CLI all consume this):**

```go
type DiagKind string
const (KindError DiagKind = "error"; KindAppTier DiagKind = "app-tier")
type Diagnostic struct {
	File      string   `json:"file"`
	Line, Col int      `json:"line","col"`
	Kind      DiagKind `json:"kind"`
	Construct string   `json:"construct,omitempty"`
	Message   string   `json:"message"`
}
type Options struct{ Data map[string]any } // protocol "data" — merged over $site
type Result struct {
	Files       map[string]string   // "index.html" → rendered bytes
	Deps        map[string][]string // output → source files used
	Diagnostics []Diagnostic
}
func Compile(fs vfs.FS, entries []string, opts Options) Result
// echo.go
func Escape(s string) string      // Laravel e(): & < > " ' → entities; invalid UTF-8 → U+FFFD
func Stringify(v expr.Value) (string, error) // PHP echo: nil/false → "", true → "1", float shortest round-trip; error for []Value/map
```

Output naming: `resources/views/pages/about.blade.html` → `about.html`; `pages/index.blade.html` → `index.html`; nested `pages/docs/intro.blade.html` → `docs/intro.html`.

**The newline rule (empirical, this task settles it):** PHP consumes one `\n` immediately after `?>`. Blade compiles both directives and echoes to `<?php … ?>`, so the assumption is: after a Directive construct AND after an Echo/RawEcho, one immediately-following `\n` is dropped; `{{-- comments --}}` are stripped pre-compile so they do NOT eat newlines. **Do not trust this paragraph** — Step 4 probes the oracle first and the fixture encodes whatever real Blade actually does.

- [ ] **Step 1: Remove the harness build tag; write render unit tests**

```go
func TestEscape(t *testing.T) {
	if got := Escape(`<a href="x">&'`); got != `&lt;a href=&quot;x&quot;&gt;&amp;&#039;` {
		t.Fatal(got)
	}
	if got := Escape("bad\xffutf8"); got != "bad�utf8" {
		t.Fatal(got)
	}
}

func TestStringify(t *testing.T) {
	cases := []struct{ in expr.Value; want string }{
		{nil, ""}, {false, ""}, {true, "1"},
		{int64(42), "42"}, {1.0, "1"}, {2.5, "2.5"}, {0.1, "0.1"},
	}
	for _, c := range cases {
		got, err := Stringify(c.in)
		if err != nil || got != c.want {
			t.Fatalf("Stringify(%v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := Stringify([]expr.Value{}); err == nil {
		t.Fatal("arrays must error")
	}
}
```

Run: `go test ./blade/render/` → FAIL

- [ ] **Step 2: Implement `Compile` for Text/Echo/Comment nodes** — walk entries, scan+parse each, evaluate echoes against a scope (Task 9 wires real data; for now scope = `opts.Data` flattened), apply Escape for `Echo{Raw:false}`, record deps (the entry itself), map output names. Float formatting: `strconv.FormatFloat(f, 'G', -1, 64)` then lowercase exponent handling to match PHP `serialize_precision=-1` (`1e+20` cases get a dedicated stringify test vs oracle in Step 4).

Run: `go test ./blade/render/ ./fixtures/` → render green; passthrough fixture green.

- [ ] **Step 3: Probe real Blade BEFORE writing the semantics fixtures**

```bash
mkdir -p /tmp/probe/resources/views/pages
printf 'A{{ $x }}\nB\n@if (true)\nC\n@endif\nD{{-- gone --}}\nE\n' \
  > /tmp/probe/resources/views/pages/index.blade.html
cd oracle && php artisan pocketknife:render /tmp/probe pages.index x=1   # extend render cmd: k=v pairs → view data
```

Record the exact bytes real Blade emits. That output IS `fixtures/newline-eating/expected/index.html`.

- [ ] **Step 4: Fixtures** — `echo-escaping` (`{{ '<b>&"\'' }}` etc.), `stringify` (floats/bools/null via `data`), `newline-eating` (the probe file), `comments`. For each: input + expected authored from oracle output, then:

Run: `go test ./fixtures/` → PASS (Go matches oracle bytes)
Run: `cd oracle && php artisan pocketknife:conformance ../fixtures` → all ok

- [ ] **Step 5: Adjust the renderer's newline logic to whatever Step 3 revealed** (constant table `eatsNewline[Kind]bool`), re-run both suites → PASS.

- [ ] **Step 6: Commit** — `git commit -m "feat(render): echoes, e() escaping, PHP stringification, oracle-verified newline semantics"`

---

### Task 7: Control flow — `@if`, `@foreach`, `$loop`, `@break`/`@continue` (`blade/render/control.go`)

**Files:**
- Create: `blade/render/control.go`, `blade/render/control_test.go`
- Create fixtures: `if-elseif-else/`, `foreach-nested/`, `foreach-loop-meta/`, `break-first-n/`

**Interfaces:**
- Consumes: `ast.If/Foreach/LoopJump`, `expr.Eval/Truthy`
- Produces: loop scope layering — inside `@foreach ($plans as $plan)`, `Lookup("plan")` hits the current item, `Lookup("loop")` returns `map[string]Value{"index","iteration","first","last","count","even","odd"}`; outer variables remain visible; inner loops shadow.

- [ ] **Step 1: Unit tests** — render tiny sources against a Mem fs:

```go
{`@if ($n == 1)one@elseif ($n == 2)two@else many@endif`, data{"n": 2}, "two"},
{`@foreach ($xs as $x){{ $loop->iteration }}:{{ $x }};@endforeach`, data{"xs": []any{"a", "b"}}, "1:a;2:b;"},
{`@foreach ($xs as $k => $x){{ $k }}={{ $x }};@endforeach`, data{"xs": []any{"a"}}, "0=a;"},
{`@foreach ($xs as $x)@if ($loop->first)[@endif{{ $x }}@if ($loop->last)]@endif@endforeach`,
	data{"xs": []any{1, 2, 3}}, "[123]"},
{`@foreach ($xs as $x){{ $x }}@break($loop->iteration == 2)@endforeach`, data{"xs": []any{1, 2, 3}}, "12"},
{`@foreach ($xs as $x)@continue($x == 2){{ $x }}@endforeach`, data{"xs": []any{1, 2, 3}}, "13"},
```

Note `$loop->iteration` — `$loop` is a `map[string]Value` and `->` must traverse maps (already the expr behavior for `map[string]Value`).

Run → FAIL, implement, run → PASS.

- [ ] **Step 2: Fixtures with oracle** — port the four cases above into fixture dirs (data via `case.json` `"data"`). `@foreach` over a non-array value → diagnostic `error` + block renders empty (fixture `foreach-nonarray`, `"oracle": false` since the oracle would throw).

Run: `go test ./fixtures/` and `php artisan pocketknife:conformance ../fixtures` → PASS

- [ ] **Step 3: Commit** — `git commit -m "feat(render): control flow with PHP-parity \$loop and break/continue"`

---

### Task 8: Components — resolution, `@props`, slots, recursion cap (`blade/render/component.go`)

**Files:**
- Create: `blade/render/component.go`, `blade/render/component_test.go`
- Create fixtures: `component-basic/`, `component-props-defaults/`, `component-bound-attrs/`, `layout-slots/`, `component-missing/` (`"oracle": false`), `component-recursion/` (`"oracle": false`)

**Interfaces:**
- Consumes: `ast.Component/Props`, vfs, expr
- Produces: resolution `sections.hero` → `resources/views/components/sections/hero.blade.html` (exact match only); `Deps` gains every resolved component file; recursion cap **16** → `Diagnostic{Kind: "error", Construct: "component-recursion"}`; missing → output marker `<!-- pocketknife:missing component "sections.hero" -->` **byte-exact** + `Diagnostic{Kind: "error", Construct: "component-missing"}`.

Component scope rules (each is a test):
1. `@props(['heading' => 'Default'])` — parse via a dedicated small parser over the literal-array grammar (`['k' => scalar, 'bare']`); bare entries have no default. Declared props become variables; static attr strings override defaults; bound attrs (`:plans="$plans"`) evaluate in the PARENT scope and pass values through.
2. Undeclared attributes are dropped, with `Diagnostic{Kind: "app-tier", Construct: "undeclared-attribute"}` **downgraded to a lint-only warning list?** No — spec says lint warns; renderer emits NO diagnostic for undeclared attrs (outputs agree with real Blade). `pocketknife lint` (Task 12) surfaces them.
3. A component body sees ONLY its props (+ `$site`, Task 9) — never the caller's locals. Fixture `component-scope-isolation` proves `{{ $callerVar ?? 'unset' }}` renders `unset`.
4. `{{ $slot }}` renders the caller's body, already rendered in the CALLER's scope. Named slots likewise.
5. `@props` not first-thing-in-file → `Diagnostic{Kind: "error"}`.

- [ ] **Step 1: Unit tests for props parsing + scope isolation** (table-driven as in Task 7). Run → FAIL. Implement resolution + rendering. Run → PASS.

- [ ] **Step 2: Fixtures.** `layout-slots` is the keystone — a real page:

`fixtures/layout-slots/input/resources/views/pages/index.blade.html`:
```html
<x-layouts.main title="Home">
<x-slot:head><meta name="x" content="y"></x-slot>
<x-sections.hero heading="Welcome"/>
</x-layouts.main>
```

`.../components/layouts/main.blade.html`:
```html
@props(['title' => 'Untitled'])
<!DOCTYPE html>
<html>
<head><title>{{ $title }}</title>{{ $head ?? '' }}</head>
<body>{{ $slot }}</body>
</html>
```

`.../components/sections/hero.blade.html`:
```html
@props(['heading' => 'Hello'])
<section><h1>{{ $heading }}</h1></section>
```

**Author `expected/index.html` from the oracle** (`php artisan pocketknife:render …`) — named-slot and `$slot` whitespace behavior must be real Blade's, not guessed. Watch specifically: does `{{ $slot }}` escape? (Real Blade: `$slot` is an `HtmlString`, so NO — `{{ $slot }}` emits raw. The Go renderer must special-case slot/head values as pre-rendered raw HTML.)

Run both suites → PASS.

- [ ] **Step 3: Commit** — `git commit -m "feat(render): anonymous components, @props, slots, recursion cap, missing markers"`

---

### Task 9: Data bindings — `$site`, collections, content (+ `laravel/` parity) (`content/markdown.go`)

**Files:**
- Create: `content/markdown.go`, `content/markdown_test.go`
- Modify: `blade/render/render.go` (root scope assembly), `laravel/src/PocketknifeServiceProvider.php`, create `laravel/src/SiteData.php`
- Create fixtures: `data-site/`, `data-collections/`, `data-content/`

**Interfaces:**
- Produces (Go side, root scope for pages and layouts): `$site` from `resources/data/site.json` (object; `opts.Data` merges over it, keys win); `$<basename>` per `resources/data/collections/*.json` (must be a JSON array → else `error` diagnostic); `$<dirname>` per `resources/data/content/<dirname>/*.md` — items are maps with frontmatter keys + `content` (goldmark-rendered HTML, GFM tables + raw HTML passthrough) + `link` (`/post/hello` for `post/hello.md`), ordered by filename ascending. Components do NOT receive these (only `$site` — spec §3.5 namespaces it as globally safe).
- Produces (PHP side, `SiteData::share($viewFactory, $root)`): identical bindings implemented with `league/commonmark`? **No** — goldmark vs commonmark would diverge on edge cases. The oracle must render `content` items through the SAME markdown output. Solution: `content/markdown.go` is authoritative; the conformance fixtures for content use simple markdown (headings, paragraphs, bold, links, lists) where CommonMark implementations agree, and `laravel/` uses `league/commonmark` in GFM mode. A dedicated `markdown-parity` fixture pins the agreed subset. Divergent constructs are out of scope for v1 templates.

- [ ] **Step 1: Content package tests** — frontmatter split (`---\nkey: value\n---\n`), YAML scalars only (strings/numbers/bools — no nested maps in v1; nested → `error` diagnostic), rendering, link derivation, filename ordering. Run → FAIL → implement (goldmark + a 40-line scalar-YAML frontmatter parser; NO yaml dependency) → PASS.

- [ ] **Step 2: Root scope in Go renderer** + fixtures:

`fixtures/data-site/input/resources/data/site.json`: `{"name": "Acme", "tagline": "We build"}`
page: `<h1>{{ $site->name }}</h1><p>{{ $site->tagline }} — {{ $site->missing ?? 'n/a' }}</p>`

`fixtures/data-collections/input/resources/data/collections/plans.json`:
```json
[{"name": "Free", "price": 0}, {"name": "Pro", "price": 9}]
```
page: `@foreach ($plans as $plan)<li>{{ $plan->name }}: {{ $plan->price }}</li>@endforeach`

`data-content`: two `.md` posts, page loops `{!! $post->content !!}` + `{{ $post->link }}`.

- [ ] **Step 3: PHP parity** — `SiteData.php`: a view composer (`View::composer('*', …)`) reading the same three sources; `composer require league/commonmark` in `laravel/`. Provider boots it when `site_root` is set. Author all three fixtures' `expected/` from the oracle; run both suites → PASS.

- [ ] **Step 4: Commit** — `git commit -m "feat(data): site/collections/content bindings on both renderers"`

---

### Task 10: `@vite` static projection

**Files:**
- Modify: `blade/render/render.go` (Vite node), create `laravel/src/StaticVite.php`, modify provider
- Create fixture: `vite-css/`

**Interfaces:**
- Produces (both renderers, byte-identical): `@vite(['resources/css/site.css'])` →

```html
<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>
<style type="text/tailwindcss">CONTENTS_OF_site.css</style>
```

(no trailing newline beyond what the directive line's newline rule dictates). Multiple CSS entries → one CDN script, one `<style>` per entry, in argument order. Any `.js` entry → `Diagnostic{Kind: "app-tier", Construct: "@vite-js"}` and the entry is skipped. Missing CSS file → `error` diagnostic + empty style block omitted.
- PHP side: `StaticVite extends Illuminate\Foundation\Vite` overriding `__invoke($entrypoints, $buildDirectory = null): HtmlString` with the same emission, bound as singleton when `config('pocketknife.static_projection')` is true.

- [ ] **Step 1: Go test + implementation** (unit: argument parsing over the `['a', 'b']` literal — reuse the props literal-array parser). Run → PASS.
- [ ] **Step 2: PHP `StaticVite` + provider binding; `vite-css` fixture** (input has a small `resources/css/site.css` with an `@import "tailwindcss";` line + one custom rule); expected authored from oracle. Both suites → PASS.
- [ ] **Step 3: Commit** — `git commit -m "feat(vite): static projection on both renderers"`

---

### Task 11: Diagnostics — app-tier classification, undefined vars, fuzz hardening (`blade/render/diagnostics.go`)

**Files:**
- Create: `blade/render/diagnostics.go`, `blade/render/diagnostics_test.go`
- Create fixtures: `app-tier-directives/` (`"oracle": false`), `undefined-var/` (`"oracle": false`)

**Interfaces:**
- Produces: `ast.Unknown` (any directive outside the subset: `@auth`, `@php`, `@include`, `@extends`, `@section`, …) → `Diagnostic{Kind: "app-tier", Construct: "@auth"}`; the directive renders as NOTHING (dropped from output, body content of paired unknowns kept as text? NO — unknown directives are dropped as single tokens; their "bodies" were never paired by the parser, so following text flows through untouched). Expression `SyntaxError{AppTier: true}` (function calls, `?->`, `.`) → app-tier with `Construct: "expression"`. `$attributes` → app-tier `Construct: "$attributes"`. Echoed undefined var (no `??`) → `error`, `Construct: "undefined-variable"`, echo renders empty. All diagnostics dedupe on (file,line,col,construct).

- [ ] **Step 1: Tests + fixtures** asserting exact Diagnostic tuples via `case.json` `"diagnostics"` (harness already matches them, Task 1). A page using `@auth…@endauth`, `{{ route('home') }}`, `{{ $nope }}` yields exactly 4 diagnostics (`@auth`, `@endauth`, `expression`, `undefined-variable`) and still renders the surrounding HTML. Run → FAIL → implement → PASS.
- [ ] **Step 2: End-to-end fuzz** — `FuzzCompile` in render: arbitrary bytes as a page, `Compile` must never panic. 60s clean.
- [ ] **Step 3: Commit** — `git commit -m "feat(diagnostics): app-tier classification and undefined-variable errors"`

---

### Task 12: Protocol + CLI (`protocol/`, `cmd/pocketknife`)

**Files:**
- Create: `protocol/protocol.go`, `protocol/protocol_test.go`, `cmd/pocketknife/main.go`, `blade/version.go`

**Interfaces:**
- Produces (spec §6, exact JSON):

```go
type Input struct {
	Files   map[string]string `json:"files"`
	Data    map[string]any    `json:"data,omitempty"`
	Entries []string          `json:"entries,omitempty"` // empty → every resources/views/pages/**.blade.html
	Options struct{ URL string `json:"url,omitempty"` } `json:"options"`
}
type Output struct {
	Engine      string              `json:"engine"` // blade.Version = "0.1.0"
	Files       map[string]string   `json:"files"`
	Deps        map[string][]string `json:"deps"`
	Diagnostics []render.Diagnostic `json:"diagnostics"`
}
func Run(in io.Reader, out io.Writer) error // decode → vfs.Mem → render.Compile → encode
```

- CLI: `pocketknife compile` (protocol over stdin/stdout), `pocketknife lint <dir>` (compile all pages from `vfs.Dir`, print diagnostics human-readably — `file:line:col [kind] message`, plus undeclared-attribute warnings — exit 1 on any `error`, exit 0 with output on only `app-tier`), `pocketknife version`. Hand-rolled arg handling, no CLI framework.

- [ ] **Step 1: Protocol round-trip test** — feed an Input with a one-page site + one app-tier construct; assert Files/Deps/Diagnostics JSON. Run → FAIL → implement → PASS.
- [ ] **Step 2: CLI smoke** — `go build ./cmd/pocketknife && echo '{"files":{...}}' | ./pocketknife compile` and `./pocketknife lint fixtures/app-tier-directives/input`; assert outputs in a `main_test.go` using `os/exec` against the built binary (or `go run`).
- [ ] **Step 3: Commit** — `git commit -m "feat(cli): compile protocol, lint, version"`

---

### Task 13: CI (`.github/workflows/ci.yml`)

**Files:** Create: `.github/workflows/ci.yml`

```yaml
name: ci
on: [push, pull_request]
jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go vet ./... && test -z "$(gofmt -l .)"
      - run: go test ./...
      - run: go test ./blade/scanner -fuzz=FuzzScan -fuzztime=20s
      - run: go test ./blade/parser -fuzz=FuzzParse -fuzztime=20s
  conformance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: shivammathur/setup-php@v2
        with: { php-version: '8.3' }
      - run: cd oracle && composer install --no-interaction --quiet
      - run: cd oracle && php artisan pocketknife:conformance ../fixtures
```

- [ ] **Step 1: Add workflow, push, verify both jobs green** (repo needs a GitHub remote — create `thedevdojo/pocketknife` private repo first; **ask Tony before creating anything on GitHub**).
- [ ] **Step 2: Commit** — `git commit -m "ci: go + oracle conformance jobs"`

---

### Task 14: Convert 2–3 templates (`~/devdojo/templates`, branch `blade-dev`)

**Files (in the templates repo):**
- Modify: `blank/` — full conversion; `template.json` gains `"dialect": "blade"`
- Modify: one landing template (`landing-lumen/`) and one multi-page template — before starting, run `ls <slug>/files/src/views/pages/` across templates and pick one with ≥3 pages; `aria` is the first candidate to check
- Create (in pocketknife): `fixtures/template-blank/` — the converted blank template as a full fixture

**Conversion recipe (per template — this is the manual transpile):**

| Classic | Blade dialect |
|---|---|
| `src/views/pages/x.html` | `resources/views/pages/x.blade.html` |
| `src/views/layouts/main.html` + `{slot}` | `resources/views/components/layouts/main.blade.html` + `{{ $slot }}` + `@props([...])` for its attrs |
| `<layout src="main.html" title="X">…</layout>` | `<x-layouts.main title="X">…</x-layouts.main>` |
| `<include src="hero.html" heading="Y">` / `components/**` | `<x-hero heading="Y"/>` under `resources/views/components/`; add `@props(['heading' => '<default from companion yml>'])` |
| `<ForEach collection="plans" as="plan">…{plan.name}…</ForEach>` | `@foreach ($plans as $plan)…{{ $plan->name }}…@endforeach`; move `count="3"` → `@break($loop->iteration == 3)`; `orderBy` → re-order the JSON file |
| `{key}` (global data) | `{{ $site->key }}`; move `src/data.json` → `resources/data/site.json` |
| `{tailwindcss}` | `@vite(['resources/css/site.css'])`; move `src/assets/css/main.css` → `resources/css/site.css` |
| `src/assets/images/**`, `src/assets/js/**` | `public/images/**`, `public/js/**` + plain `<script src="/js/…">` tags |
| `{ url('/x') }` | plain `/x` (relative URLs; the platform's UrlRewriter handles prefixing at serve time in the later phase) |

- [ ] **Step 1:** `cd ~/devdojo/templates && git checkout blade-dev`. Convert `blank` by hand per the recipe.
- [ ] **Step 2:** Verify via lint: `pocketknife lint ~/devdojo/templates/blank/files` → zero diagnostics. Render: `pocketknife` compile over the tree (small helper script or `lint` + a `--write out/` flag if trivial; otherwise pipe protocol JSON) and eyeball `index.html` in a browser.
- [ ] **Step 3:** Verify via oracle: `cd ~/devdojo/pocketknife/oracle && php artisan pocketknife:render ~/devdojo/templates/blank/files pages.index` — byte-diff against the Go output (`diff <(go-output) <(oracle-output)`). Iterate until identical.
- [ ] **Step 4:** Copy the converted blank tree into `fixtures/template-blank/` (input = the files, expected = the verified output, all pages as entries) so the full-template case is pinned in CI forever. Commit in pocketknife.
- [ ] **Step 5:** Repeat Steps 1–3 for the landing template and the multi-page template. Every diagnostic hit during conversion is a grammar-pressure signal: if a template genuinely can't be expressed, STOP and flag for a spec amendment rather than hacking around it.
- [ ] **Step 6:** Commit in templates repo: `git commit -m "feat: blade-dialect conversions — blank, landing-lumen, <multipage>"`. Do not push or open PRs without asking.

**v1 exit criteria (from the spec, verify each):**
1. `make test` green; `make conformance` green (all fixtures, both renderers, byte-identical).
2. Three converted templates render byte-identically through Go and oracle.
3. `make fuzz` — zero panics.
4. `pocketknife lint` on a real `default/` Laravel view produces accurate `app-tier` diagnostics — the tier-gate demo.

---

## Plan self-review notes (done at write time)

- **Spec coverage:** §2 structure → Tasks 1/6/14; §3.1 → T6; §3.2 → T7; §3.3 → T8; §3.4 → T5; §3.5 → T9; §3.6 → T10; §3.7 → T11; §4 → T1/3/4; §5 → T2/9/10; §6 → T12; §7 → T1/2/13; §8 templates → T14. Routing table (§2) is exercised only via explicit entries, as the spec scopes for v1.
- **Ordering deviation from spec, deliberate:** oracle moved to Task 2 so semantics fixtures are authored from real Blade output rather than assumptions (newline eating, slot escaping, float formatting are all probe-first).
- **Type consistency:** `render.Compile(fs, entries, render.Options{Data})` / `Result{Files, Deps, Diagnostics}` used identically in Tasks 1, 6, 12; `Diagnostic{Kind, Construct}` in 6/11/12; `vfs.Mem`/`vfs.Dir` in 1/7/12/14; `expr.Value/Scope/ErrUndefinedVar` in 5/6/7/11.
- **Open empirical questions are marked as probe-first steps**, not assumptions: newline rule (T6 S3), missing-property vs undefined-var (T5 S2 note), `$slot` rawness (T8 S2), float exponent formatting (T6 S2).




