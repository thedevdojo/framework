// Package main_test drives the built devdojo binary end to end via
// os/exec — the CLI's contract is its process behavior (stdin/stdout/stderr,
// exit code), not any Go API, so that is what these tests exercise. TestMain
// builds the binary once into a temp dir and every test shells out to it.
package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/thedevdojo/framework/blade"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "devdojo-cli-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "devdojo")
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic("go build ./cmd/devdojo failed: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

// runCLI runs the built binary with the given args and stdin, and returns
// stdout, stderr, and the process exit code (0 for a clean exit).
func runCLI(t *testing.T, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runCLIEnv(t, nil, stdin, args...)
}

// runCLIEnv is runCLI with variables added to the environment.
func runCLIEnv(t *testing.T, env []string, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		return outBuf.String(), errBuf.String(), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("failed to run %s %v: %v (stderr: %s)", binPath, args, err, errBuf.String())
	}
	return outBuf.String(), errBuf.String(), exitErr.ExitCode()
}

func TestVersion(t *testing.T) {
	stdout, stderr, code := runCLI(t, "", "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := "devdojo " + blade.Version + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestUsageNoArgs(t *testing.T) {
	stdout, stderr, code := runCLI(t, "")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on a usage error, got %q", stdout)
	}
	if !strings.Contains(strings.ToLower(stderr), "usage") {
		t.Errorf("stderr should contain usage text, got %q", stderr)
	}
}

func TestUsageUnknownCommand(t *testing.T) {
	stdout, stderr, code := runCLI(t, "", "bogus-command")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on a usage error, got %q", stdout)
	}
	if stderr == "" {
		t.Errorf("expected usage text on stderr")
	}
}

func TestCompileHappyPath(t *testing.T) {
	input := `{"files":{"src/pages/index.blade.php":"<h1>Hi</h1>"}}`
	stdout, stderr, code := runCLI(t, input, "compile")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var out struct {
		Engine string            `json:"engine"`
		Files  map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if out.Engine != blade.Version {
		t.Errorf("engine = %q, want %q", out.Engine, blade.Version)
	}
	if out.Files["index.html"] != "<h1>Hi</h1>" {
		t.Errorf("files = %#v", out.Files)
	}
}

// TestCompileErrorDiagnosticsStillExitZero: a compile whose output carries an
// error-kind diagnostic is still a successful CLI invocation — the OUTPUT
// carries the problem, the exit code does not.
func TestCompileErrorDiagnosticsStillExitZero(t *testing.T) {
	input := `{"files":{"src/pages/index.blade.php":"{{ $nope }}"}}`
	stdout, stderr, code := runCLI(t, input, "compile")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 even with an error diagnostic (stderr: %s, stdout: %s)", code, stderr, stdout)
	}
	if !strings.Contains(stdout, `"kind":"error"`) {
		t.Errorf("expected an error diagnostic in stdout, got %s", stdout)
	}
}

// TestCompileMalformedJSONExitsZero: malformed stdin becomes a protocol
// diagnostic inside the (still parseable) output, not a CLI failure.
func TestCompileMalformedJSONExitsZero(t *testing.T) {
	stdout, stderr, code := runCLI(t, "{ this is not json", "compile")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var out struct {
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0]["construct"] != "protocol" {
		t.Errorf("diagnostics = %#v, want one construct=protocol diagnostic", out.Diagnostics)
	}
}

// TestLintErrorExitsOne compiles the repo's own app-tier-directives fixture,
// which carries one error-kind diagnostic (an undefined variable) alongside
// several app-tier ones — lint must exit 1 and print every diagnostic in
// `file:line:col [kind] message` form.
func TestLintErrorExitsOne(t *testing.T) {
	stdout, stderr, code := runCLI(t, "", "lint", "../../fixtures/app-tier-directives/input")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	lineRe := regexp.MustCompile(`^src/pages/index\.blade\.php:\d+:\d+ \[(error|app-tier)\] .+$`)
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 diagnostic lines, got %d:\n%s", len(lines), stdout)
	}
	for _, l := range lines {
		if !lineRe.MatchString(l) {
			t.Errorf("line does not match file:line:col [kind] message: %q", l)
		}
	}
	if !strings.Contains(stdout, "[error]") {
		t.Errorf("expected at least one [error] line, got:\n%s", stdout)
	}
	// Diagnostic lines are sorted by file:line:col — line 2 before line 4
	// before line 5 before line 6 before line 7 before line 8 (the static-attr
	// interpolation on the <x-hero> tag).
	sorted := []string{":2:", ":4:", ":5:", ":6:", ":7:", ":8:"}
	last := -1
	for _, want := range sorted {
		idx := strings.Index(stdout, want)
		if idx == -1 {
			t.Fatalf("expected %q in output:\n%s", want, stdout)
		}
		if idx < last {
			t.Errorf("diagnostics are not sorted: %q appears before an earlier line", want)
		}
		last = idx
	}
}

// TestLintAppTierOnlyExitsZero: a tree whose only diagnostics are app-tier
// (no engine-fatal error) exits 0 even though it prints output.
func TestLintAppTierOnlyExitsZero(t *testing.T) {
	dir := t.TempDir()
	pages := filepath.Join(dir, "src", "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "<div>\n@auth\nWelcome\n@endauth\n</div>\n"
	if err := os.WriteFile(filepath.Join(pages, "index.blade.php"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "[app-tier]") {
		t.Errorf("expected an [app-tier] line, got %q", stdout)
	}
	if strings.Contains(stdout, "[error]") {
		t.Errorf("did not expect an [error] line, got %q", stdout)
	}
}

// TestLintCleanTreeExitsZero: no diagnostics at all → exit 0, no output.
func TestLintCleanTreeExitsZero(t *testing.T) {
	dir := t.TempDir()
	pages := filepath.Join(dir, "src", "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "index.blade.php"), []byte("<h1>Hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected no diagnostic output, got %q", stdout)
	}
}

// writeSite writes a set of relative-path files under root, creating parent
// directories as needed — a small helper for the multi-file lint cases.
func writeSite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLintUndeclaredAttributeWarnsExitsZero: an undeclared component attribute
// prints a [warning] line but leaves the exit code at 0 — a warning is
// advisory, not a failure.
func TestLintUndeclaredAttributeWarnsExitsZero(t *testing.T) {
	dir := t.TempDir()
	writeSite(t, dir, map[string]string{
		"src/pages/index.blade.php":              `<x-sections.hero heading="Hi" subtitle="typo"/>`,
		"src/components/sections/hero.blade.php": "@props(['heading' => 'Default'])\n<h1>{{ $heading }}</h1>",
	})
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for warnings-only (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	want := `[warning] undeclared attribute "subtitle" on <x-sections.hero> (not in @props)`
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
}

// TestLintDeclaredAndBoundAttributesSilent: a declared static attribute and a
// bound attribute produce no warning at all.
func TestLintDeclaredAndBoundAttributesSilent(t *testing.T) {
	dir := t.TempDir()
	writeSite(t, dir, map[string]string{
		// $site is always bound (empty object with no site.json), so the bound
		// attribute evaluates cleanly — the point is that neither the declared
		// static attr nor the bound attr produces a lint warning.
		"src/pages/index.blade.php":              `<x-sections.hero heading="Hi" :rows="$site"/>`,
		"src/components/sections/hero.blade.php": "@props(['heading' => 'Default'])\n<h1>{{ $heading }}</h1>",
	})
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if strings.Contains(stdout, "[warning]") {
		t.Errorf("expected no [warning] line, got %q", stdout)
	}
}

func TestLintUsageMissingDir(t *testing.T) {
	stdout, stderr, code := runCLI(t, "", "lint")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty, got %q", stdout)
	}
	if stderr == "" {
		t.Errorf("expected a usage message on stderr")
	}
}

// TestLintNonexistentDirExitsTwo: `lint <dir>` on a path that does not exist
// must fail loudly (clear stderr, exit 2), not silently exit 0 with no output
// as if the tree were clean.
func TestLintNonexistentDirExitsTwo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	stdout, stderr, code := runCLI(t, "", "lint", missing)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty, got %q", stdout)
	}
	if stderr == "" {
		t.Errorf("expected an error message on stderr for a missing directory")
	}
}

// TestLintNotADirectoryExitsTwo: `lint <file>` where the path exists but is a
// regular file is also a usage error (exit 2, clear stderr).
func TestLintNotADirectoryExitsTwo(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "", "lint", file)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty, got %q", stdout)
	}
	if stderr == "" {
		t.Errorf("expected an error message on stderr for a non-directory path")
	}
}

// TestCompileHTMLSyntax: a page ending in .html is written in the Studio syntax,
// and compiles through the same protocol with nothing extra in the request.
func TestCompileHTMLSyntax(t *testing.T) {
	input := `{"files":{` +
		`"src/pages/index.html":"<Layout src=\"main.html\" title=\"Home\"><Foreach posts=\"post\"><Card :title=\"post.title\" /></Foreach></Layout>",` +
		`"src/components/layouts/main.html":"<title>{{ title }}</title><Slot />",` +
		`"src/components/card.html":"<Props title /><h2>{{ title }}</h2>"` +
		`},"data":{"posts":[{"title":"One"},{"title":"Two"}]}}`
	stdout, stderr, code := runCLI(t, input, "compile")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var out struct {
		Files       map[string]string `json:"files"`
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if len(out.Diagnostics) != 0 {
		t.Errorf("diagnostics = %s; want none", out.Diagnostics)
	}
	if got, want := out.Files["index.html"], "<title>Home</title><h2>One</h2><h2>Two</h2>"; got != want {
		t.Errorf("index.html = %q; want %q", got, want)
	}
}

// TestLintHTMLSyntax: lint finds .html pages on its own, and reports on them
// in the words their author wrote — tags, not directives, and no "$".
func TestLintHTMLSyntax(t *testing.T) {
	dir := t.TempDir()
	writeSite(t, dir, map[string]string{
		"src/pages/index.html":              "<SectionsHero heading=\"Hi\" subtitle=\"typo\" />\n<If condition=\"titel\">x</If>\n<Foreach posts>y</Foreach>",
		"src/components/sections/hero.html": "<Props heading=\"Default\" />\n<h1>{{ heading }}</h1>",
	})
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for an error (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	for _, want := range []string{
		`src/pages/index.html:1:28 [warning] undeclared attribute "subtitle" on <SectionsHero> (not in its <Props>)`,
		`src/pages/index.html:2:1 [error] undefined-variable: undefined variable titel`,
		`src/pages/index.html:3:1 [error] <Foreach>: <Foreach> needs a list and a name for each item, like <Foreach posts="post">`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "@") || strings.Contains(stdout, "$") {
		t.Errorf("a Studio-syntax report should not speak Blade:\n%s", stdout)
	}
}

// tempSite writes a site's files into a fresh temp dir and returns it.
func tempSite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeSite(t, dir, files)
	return dir
}

// tree lists every file under dir, relative, sorted, as one string.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return strings.Join(out, "\n")
}

var convertSite = map[string]string{
	"src/pages/index.blade.php":             "<x-layouts.main>\n@foreach ($site->items as $item)\n<x-card :title=\"$item\" />\n@endforeach\n</x-layouts.main>\n",
	"src/components/layouts/main.blade.php": "<main>{{ $slot }}</main>\n",
	"src/components/card.blade.php":         "@props(['title' => 'T'])\n<h1>{{ $title }}</h1>\n",
	"src/data/site.json":                    `{"items": ["a", "b"]}`,
}

// TestConvertOnDisk: `convert <dir> --to studio` (the HTML syntax's older
// name, still taken) rewrites a site in place — the
// new files appear, the old ones go, everything else is left alone — and
// `--to blade` brings it back byte for byte.
func TestConvertOnDisk(t *testing.T) {
	dir := tempSite(t, convertSite)
	stdout, stderr, code := runCLI(t, "", "convert", dir, "--to", "studio")
	if code != 0 {
		t.Fatalf("exit code = %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	want := "src/components/card.html\nsrc/components/layouts/main.html\nsrc/data/site.json\nsrc/pages/index.html"
	if got := tree(t, dir); got != want {
		t.Errorf("files after converting:\n%s\nwant:\n%s", got, want)
	}
	page, _ := os.ReadFile(filepath.Join(dir, "src/pages/index.html"))
	if want := "<Layout src=\"main.html\">\n<Foreach site.items=\"item\">\n<Card :title=\"item\" />\n</Foreach>\n</Layout>\n"; string(page) != want {
		t.Errorf("index.html = %q\nwant %q", page, want)
	}
	if !strings.Contains(stdout, "Converted 3 files to the HTML syntax") {
		t.Errorf("stdout = %q", stdout)
	}

	stdout, stderr, code = runCLI(t, "", "convert", "--to=blade", dir)
	if code != 0 {
		t.Fatalf("back: exit code = %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	for rel, body := range convertSite {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || string(got) != body {
			t.Errorf("%s did not come back: %q (%v)", rel, got, err)
		}
	}
	if !strings.Contains(stdout, "Converted 3 files to Blade") {
		t.Errorf("stdout = %q", stdout)
	}
}

// TestConvertDryRun: --dry-run says what would happen and touches nothing.
// It asks with `--to html`, the HTML syntax's name.
func TestConvertDryRun(t *testing.T) {
	dir := tempSite(t, convertSite)
	before := tree(t, dir)
	stdout, stderr, code := runCLI(t, "", "convert", dir, "--to", "html", "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if got := tree(t, dir); got != before {
		t.Errorf("--dry-run changed the site:\n%s", got)
	}
	for _, want := range []string{
		"src/pages/index.blade.php → src/pages/index.html",
		"Would convert 3 files to the HTML syntax",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
}

// TestConvertRefusedTouchesNothing: one file that cannot be converted and the
// whole site is left exactly as it was, with the reason and exit code 1.
func TestConvertRefusedTouchesNothing(t *testing.T) {
	files := map[string]string{}
	for k, v := range convertSite {
		files[k] = v
	}
	files["src/components/gate.blade.php"] = "x\n@auth\nsecret\n@endauth\n"
	dir := tempSite(t, files)
	before := tree(t, dir)
	stdout, stderr, code := runCLI(t, "", "convert", dir, "--to", "html")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if got := tree(t, dir); got != before {
		t.Errorf("a refused conversion changed the site:\n%s", got)
	}
	for _, want := range []string{
		"src/components/gate.blade.php:2:1 @auth needs real Laravel, which the HTML syntax does not have",
		"Nothing was converted",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
}

// TestConvertNothingToDo: converting a site that is already there is fine.
func TestConvertNothingToDo(t *testing.T) {
	dir := tempSite(t, convertSite)
	stdout, _, code := runCLI(t, "", "convert", dir, "--to", "blade")
	if code != 0 || !strings.Contains(stdout, "Nothing to convert") {
		t.Errorf("exit code = %d, stdout = %q", code, stdout)
	}
}

// TestConvertUsage: a missing or unknown --to, or a folder that is not there,
// is a usage error (exit 2) and nothing is read.
func TestConvertUsage(t *testing.T) {
	dir := tempSite(t, convertSite)
	for _, args := range [][]string{
		{"convert", dir},
		{"convert", dir, "--to", "twig"},
		{"convert", dir, "--to"},
		{"convert", filepath.Join(dir, "nope"), "--to", "html"},
		{"convert", dir, "extra", "--to", "html"},
		{"convert", dir, "--to", "html", "--force"},
	} {
		_, stderr, code := runCLI(t, "", args...)
		if code != 2 || stderr == "" {
			t.Errorf("%v: exit code = %d, stderr = %q; want 2 and a message", args[1:], code, stderr)
		}
	}
	_, stderr, _ := runCLI(t, "", "convert", dir, "--to", "twig")
	for _, want := range []string{`unknown syntax "twig"`, "--to html or --to blade", "--to html|blade"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("an unknown syntax should name the two it can use; stderr is missing %q:\n%s", want, stderr)
		}
	}
}

// TestUsageNamesBothSyntaxes: the help says which syntax each extension is
// read in, by the names the docs use.
func TestUsageNamesBothSyntaxes(t *testing.T) {
	_, stderr, code := runCLI(t, "")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	for _, want := range []string{"convert <dir> --to html|blade", ".blade.php     Blade ", ".html          HTML "} {
		if !strings.Contains(stderr, want) {
			t.Errorf("usage is missing %q:\n%s", want, stderr)
		}
	}
}

// TestConvertProtocol: with no folder, convert speaks JSON on stdin and stdout
// the way compile does, for a host that holds the site in memory.
func TestConvertProtocol(t *testing.T) {
	in := `{"to": "blade", "files": {"src/pages/index.html": "<If featured> yes </If>\n"}}`
	stdout, stderr, code := runCLI(t, in, "convert")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	var out struct {
		Files   map[string]string `json:"files"`
		Removed []string          `json:"removed"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got := out.Files["src/pages/index.blade.php"]; got != "@if ($featured) yes @endif\n" {
		t.Errorf("converted page = %q", got)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "src/pages/index.html" {
		t.Errorf("removed = %v", out.Removed)
	}
}

// ---- src/, the older folders, and `migrate` ---------------------------------

var oldShapeSite = map[string]string{
	"resources/views/pages/index.blade.php":             "<x-layouts.main>\n<h1>{{ $site->name }}</h1>\n</x-layouts.main>\n",
	"resources/views/components/layouts/main.blade.php": "<html><head>@vite(['resources/css/site.css'])</head><body>{{ $slot }}</body></html>\n",
	"resources/css/site.css":                            "body { color: red }",
	"resources/data/site.json":                          `{"name": "Acme"}`,
	"public/robots.txt":                                 "User-agent: *\n",
}

// TestLintTellsAnOldFolderOnceAndStaysClean: a site in resources/ still lints
// clean (nothing on stdout, exit 0). What it is told goes to stderr, once.
func TestLintTellsAnOldFolderOnceAndStaysClean(t *testing.T) {
	dir := tempSite(t, oldShapeSite)
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 || stdout != "" {
		t.Fatalf("exit %d, stdout %q (stderr: %s)", code, stdout, stderr)
	}
	if strings.Count(stderr, "resources/") == 0 || !strings.Contains(stderr, "devdojo migrate") {
		t.Fatalf("stderr = %q", stderr)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Fatalf("expected one line of notice, got %q", stderr)
	}
}

// TestLintASrcSiteIsSilent: the current shape has nothing to be told.
func TestLintASrcSiteIsSilent(t *testing.T) {
	dir := tempSite(t, map[string]string{
		"src/pages/index.html":     "<Card />\n",
		"src/components/card.html": "<b>card</b>\n",
	})
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestMigrateOnDisk: `migrate <dir>` moves a site into the flat src/ shape in
// place, rewrites the stylesheet entry, leaves everything else alone, and the
// site then lints with nothing left to say.
func TestMigrateOnDisk(t *testing.T) {
	dir := tempSite(t, oldShapeSite)
	before, _, _ := runCLI(t, compileRequest(t, dir), "compile")

	stdout, stderr, code := runCLI(t, "", "migrate", dir)
	if code != 0 {
		t.Fatalf("exit code = %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	want := "public/robots.txt\nsrc/components/layouts/main.blade.php\nsrc/css/site.css\nsrc/data/site.json\nsrc/pages/index.blade.php"
	if got := tree(t, dir); got != want {
		t.Errorf("files after moving:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources")); !os.IsNotExist(err) {
		t.Errorf("the empty resources/ folder was left behind")
	}
	layout, _ := os.ReadFile(filepath.Join(dir, "src/components/layouts/main.blade.php"))
	if !strings.Contains(string(layout), "@vite(['src/css/site.css'])") {
		t.Errorf("layout = %q", layout)
	}
	if !strings.Contains(stdout, "Moved 4 files to src/") {
		t.Errorf("stdout = %q", stdout)
	}

	after, _, _ := runCLI(t, compileRequest(t, dir), "compile")
	if pageOf(t, before) == "" || pageOf(t, before) != pageOf(t, after) {
		t.Errorf("the page changed:\nbefore %q\nafter  %q", pageOf(t, before), pageOf(t, after))
	}
	if out, errOut, code := runCLI(t, "", "lint", dir); code != 0 || out != "" || errOut != "" {
		t.Errorf("lint after moving: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// compileRequest reads a site off disk into a compile request.
func compileRequest(t *testing.T, dir string) string {
	t.Helper()
	files := map[string]string{}
	for _, rel := range strings.Split(tree(t, dir), "\n") {
		body, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		files[rel] = string(body)
	}
	raw, _ := json.Marshal(map[string]any{"files": files})
	return string(raw)
}

func pageOf(t *testing.T, compiled string) string {
	t.Helper()
	var out struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(compiled), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, compiled)
	}
	return out.Files["index.html"]
}

// TestMigrateDryRun: --dry-run lists every move and touches nothing.
func TestMigrateDryRun(t *testing.T) {
	dir := tempSite(t, oldShapeSite)
	was := tree(t, dir)
	stdout, stderr, code := runCLI(t, "", "migrate", dir, "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "resources/views/pages/index.blade.php → src/pages/index.blade.php") ||
		!strings.Contains(stdout, "Would move 4 files") || !strings.Contains(stdout, "Nothing was changed") {
		t.Errorf("stdout = %q", stdout)
	}
	if got := tree(t, dir); got != was {
		t.Errorf("--dry-run changed the site:\n%s", got)
	}
}

// TestMigrateNothingToDo: a site already in the current shape is left alone.
func TestMigrateNothingToDo(t *testing.T) {
	dir := tempSite(t, map[string]string{"src/pages/index.html": "<h1>Hi</h1>\n"})
	stdout, _, code := runCLI(t, "", "migrate", dir)
	if code != 0 || !strings.Contains(stdout, "Nothing to move") {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

// TestMigrateRefusedTouchesNothing: a move that would change a page is not
// made, exits 1 and leaves the folder exactly as it was.
func TestMigrateRefusedTouchesNothing(t *testing.T) {
	dir := tempSite(t, map[string]string{
		"resources/views/pages/index.blade.php": "<p>Write @@vite(['resources/css/site.css']) in your layout.</p>\n",
	})
	was := tree(t, dir)
	stdout, _, code := runCLI(t, "", "migrate", dir)
	if code != 1 || !strings.Contains(stdout, "would render differently") || !strings.Contains(stdout, "Nothing was moved") {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	if got := tree(t, dir); got != was {
		t.Errorf("a refused move changed the site:\n%s", got)
	}
}

// TestMigrateProtocol: with no directory, migrate speaks JSON, for a host that
// holds the site in memory.
func TestMigrateProtocol(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"files": oldShapeSite})
	stdout, stderr, code := runCLI(t, string(raw), "migrate")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	var out struct {
		Engine string `json:"engine"`
		Moves  []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"moves"`
		Files       map[string]string `json:"files"`
		Diagnostics []any             `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if out.Engine != blade.Version || len(out.Moves) != 4 || len(out.Diagnostics) != 0 {
		t.Fatalf("got %+v", out)
	}
	if out.Moves[0].From != "resources/css/site.css" || out.Moves[0].To != "src/css/site.css" {
		t.Errorf("first move = %+v", out.Moves[0])
	}
	if body := out.Files["src/components/layouts/main.blade.php"]; !strings.Contains(body, "@vite(['src/css/site.css'])") || len(out.Files) != 1 {
		t.Errorf("files = %q", out.Files)
	}
}

func TestUsageNamesMigrate(t *testing.T) {
	_, stderr, _ := runCLI(t, "")
	if !strings.Contains(stderr, "migrate <dir>") {
		t.Errorf("usage = %q", stderr)
	}
}

// A dynamic page is linted the way serve and the platform compile it: its
// collection's name bound to one entry, and `entries` to the whole list, so a
// page that also lists the others lints clean. A page that names something
// truly missing still does not.
func TestLintBindsEntriesInADynamicPage(t *testing.T) {
	files := map[string]string{
		"src/pages/blog/[posts.slug].html":     "<h1>{{ posts.title }}</h1>\n<Foreach entries=\"post\">\n<a href=\"/blog/{{ post.slug }}\">{{ post.title }}</a>\n</Foreach>\n",
		"src/pages/news/[news.slug].blade.php": "<h1>{{ $news->title }}</h1>\n@foreach ($entries as $item)\n{{ $item->title }}\n@endforeach\n",
		"src/data/collections/posts.json":      `[{"slug": "a", "title": "A"}, {"slug": "b", "title": "B"}]`,
		"src/data/collections/news.json":       `[{"slug": "n", "title": "N"}]`,
	}
	dir := tempSite(t, files)
	stdout, stderr, code := runCLI(t, "", "lint", dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want a silent 0", code, stdout, stderr)
	}

	writeSite(t, dir, map[string]string{"src/pages/blog/[posts.slug].html": "<h1>{{ posts.title }}{{ nope }}</h1>\n"})
	stdout, _, code = runCLI(t, "", "lint", dir)
	if code != 1 || !strings.Contains(stdout, "nope") {
		t.Errorf("a missing name in a dynamic page: exit %d, stdout %q", code, stdout)
	}
}
