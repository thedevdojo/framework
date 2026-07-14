// Package main_test drives the built pocketknife binary end to end via
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

	"github.com/thedevdojo/pocketknife/blade"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pocketknife-cli-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "pocketknife")
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic("go build ./cmd/pocketknife failed: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

// runCLI runs the built binary with the given args and stdin, and returns
// stdout, stderr, and the process exit code (0 for a clean exit).
func runCLI(t *testing.T, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
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
	want := "pocketknife " + blade.Version + "\n"
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
	input := `{"files":{"resources/views/pages/index.blade.html":"<h1>Hi</h1>"}}`
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
	input := `{"files":{"resources/views/pages/index.blade.html":"{{ $nope }}"}}`
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
	lineRe := regexp.MustCompile(`^resources/views/pages/index\.blade\.html:\d+:\d+ \[(error|app-tier)\] .+$`)
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
	pages := filepath.Join(dir, "resources", "views", "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "<div>\n@auth\nWelcome\n@endauth\n</div>\n"
	if err := os.WriteFile(filepath.Join(pages, "index.blade.html"), []byte(src), 0o644); err != nil {
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
	pages := filepath.Join(dir, "resources", "views", "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "index.blade.html"), []byte("<h1>Hi</h1>"), 0o644); err != nil {
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
