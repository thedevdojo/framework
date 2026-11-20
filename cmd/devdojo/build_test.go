package main_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// buildSite is a small site with every kind of page `build` writes: the home
// page, a flat page, a folder's index page, the 404 page, and a dynamic page
// with one good entry, one more, and two that cannot be addresses.
func buildSite() map[string]string {
	return map[string]string{
		"src/components/layouts/main.html": "<!doctype html>\n<html>\n<head>\n<title>{site.name}</title>\n" +
			"<Vite src=\"src/css/site.css\" />\n</head>\n<body>\n<Slot />\n</body>\n</html>\n",
		"src/components/badge.html":        "<span class=\"only-in-a-component\">new</span>\n",
		"src/pages/index.html":             "<Layout src=\"main.html\">\n<h1 class=\"text-brand\">Home</h1>\n</Layout>\n",
		"src/pages/about.html":             "<Layout src=\"main.html\">\n<h1>About</h1>\n</Layout>\n",
		"src/pages/docs/index.html":        "<Layout src=\"main.html\">\n<h1>Docs</h1>\n</Layout>\n",
		"src/pages/404.html":               "<Layout src=\"main.html\">\n<h1>Not here</h1>\n</Layout>\n",
		"src/pages/blog/[posts.slug].html": "<Layout src=\"main.html\">\n<h1>{posts.title}</h1>\n</Layout>\n",
		"src/data/collections/posts.json": `[{"slug": "hello", "title": "Hello"}, {"slug": "second", "title": "Second"},` +
			` {"slug": "a/b", "title": "Slash"}, {"title": "No slug"}]`,
		"src/data/site.json":  `{"name": "Test site", "url": "https://example.com"}`,
		"src/css/site.css":    "@theme { --color-brand: #123456; }\n",
		"public/favicon.svg":  `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		"public/images/a.png": "\x89PNG\r\n\x1a\nnot-really",
		"README.md":           "not part of the site",
		"notes/private.txt":   "not part of the site either",
	}
}

// fakeTailwind writes a stand-in for Tailwind's CLI: it writes a stylesheet
// that says it was built, holds the entry it was given, and lists the files
// it was told to look in for class names.
func fakeTailwind(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for Tailwind is a shell script")
	}
	if script == "" {
		script = `while [ $# -gt 0 ]; do case "$1" in --input) in="$2"; shift;; --output) out="$2"; shift;; esac; shift; done
{ echo "/* built */"; cat "$in"; cat src/*; } > "$out"`
	}
	path := filepath.Join(t.TempDir(), "tailwindcss")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runBuild(t *testing.T, tailwind string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	env := []string{"DEVDOJO_TAILWIND=" + tailwind}
	return runCLIEnv(t, env, "", append([]string{"build"}, args...)...)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

const cdnScript = `<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>`

func TestBuildWritesTheSite(t *testing.T) {
	dir := tempSite(t, buildSite())
	stdout, stderr, code := runBuild(t, fakeTailwind(t, ""), dir)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr should be empty for a clean build: %s", stderr)
	}
	out := filepath.Join(dir, "dist")
	got := tree(t, out)
	css := regexp.MustCompile(`assets/site\.[0-9a-f]{12}\.css`).FindString(got)
	if css == "" {
		t.Fatalf("no built stylesheet among:\n%s", got)
	}
	want := strings.Join([]string{
		"404.html", "about/index.html", css, "blog/hello/index.html", "blog/second/index.html",
		"docs/index.html", "favicon.svg", "images/a.png", "index.html", "robots.txt", "sitemap.xml",
	}, "\n")
	if got != want {
		t.Errorf("dist holds:\n%s\nwant:\n%s", got, want)
	}

	home := read(t, filepath.Join(out, "index.html"))
	if !strings.Contains(home, `<link rel="stylesheet" href="/`+css+`">`) {
		t.Errorf("the home page does not link the built stylesheet:\n%s", home)
	}
	if strings.Contains(home, "cdn.jsdelivr.net") || strings.Contains(home, "text/tailwindcss") {
		t.Errorf("the home page still carries the browser script or its style block:\n%s", home)
	}
	if !strings.Contains(read(t, filepath.Join(out, "blog/hello/index.html")), "<h1>Hello</h1>") {
		t.Error("blog/hello is not the entry it was built for")
	}
	if read(t, filepath.Join(out, "images/a.png")) != buildSite()["public/images/a.png"] {
		t.Error("public/images/a.png was not copied as it is")
	}

	built := read(t, filepath.Join(out, css))
	for _, want := range []string{
		"/* built */",
		"@import \"tailwindcss\";\n@theme { --color-brand: #123456; }",
		`@source "./src/**/*";`,
		"only-in-a-component", // a component's source is looked in, not only the pages
		"text-brand",          // and the rendered pages
	} {
		if !strings.Contains(built, want) {
			t.Errorf("the stylesheet is missing %q:\n%s", want, built)
		}
	}

	if got := read(t, filepath.Join(out, "robots.txt")); got != "User-agent: *\nAllow: /\n\nSitemap: https://example.com/sitemap.xml\n" {
		t.Errorf("robots.txt = %q", got)
	}
	sitemap := read(t, filepath.Join(out, "sitemap.xml"))
	var locs []string
	for _, m := range regexp.MustCompile(`<loc>([^<]*)</loc>`).FindAllStringSubmatch(sitemap, -1) {
		locs = append(locs, m[1])
	}
	if want := "https://example.com/ https://example.com/about https://example.com/blog/hello https://example.com/blog/second https://example.com/docs"; strings.Join(locs, " ") != want {
		t.Errorf("sitemap addresses = %v, want %s", locs, want)
	}
	for _, want := range []string{"6 pages", "2 files from public/", css} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not say %q:\n%s", want, stdout)
		}
	}
}

// When Tailwind cannot be had, the pages keep the browser script and the
// build still succeeds, with one line saying so.
func TestBuildFallsBackToTheBrowserScript(t *testing.T) {
	dir := tempSite(t, buildSite())
	stdout, stderr, code := runBuild(t, filepath.Join(t.TempDir(), "no-such-tailwind"), dir)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if lines := strings.Split(strings.TrimSpace(stderr), "\n"); len(lines) != 1 || !strings.Contains(stderr, "Tailwind browser script") {
		t.Errorf("want one warning about the browser script, got:\n%s", stderr)
	}
	home := read(t, filepath.Join(dir, "dist", "index.html"))
	if !strings.Contains(home, cdnScript) || !strings.Contains(home, `<style type="text/tailwindcss">`) {
		t.Errorf("the page lost its browser script:\n%s", home)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "assets")); err == nil {
		t.Error("an assets folder was written with no stylesheet built")
	}
}

// The same when the download of Tailwind fails: nothing is cached, the build
// goes on with the browser script.
func TestBuildFallsBackWhenTailwindCannotBeDownloaded(t *testing.T) {
	dir := tempSite(t, buildSite())
	home := t.TempDir()
	env := []string{"DEVDOJO_TAILWIND=", "DEVDOJO_TAILWIND_URL=http://127.0.0.1:1", "HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "LocalAppData=" + home}
	stdout, stderr, code := runCLIEnv(t, env, "", "build", dir)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "couldn't download Tailwind") || !strings.Contains(stderr, "Tailwind browser script") {
		t.Errorf("stderr does not say the download failed and what was done instead:\n%s", stderr)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "dist", "index.html")), cdnScript) {
		t.Error("the page lost its browser script")
	}
	if got := tree(t, home); got != "" {
		t.Errorf("something was cached from a download that failed:\n%s", got)
	}
}

// Tailwind that runs and fails (a mistake in the stylesheet) stops the build:
// its message is shown and nothing is written.
func TestBuildStopsWhenTheStylesheetDoesNotBuild(t *testing.T) {
	dir := tempSite(t, buildSite())
	tw := fakeTailwind(t, `echo "CssSyntaxError: Unclosed block" >&2; exit 1`)
	_, stderr, code := runBuild(t, tw, dir)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "CssSyntaxError: Unclosed block") {
		t.Errorf("stderr does not carry Tailwind's message:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist")); err == nil {
		t.Error("dist was written")
	}
}

// One page that does not compile and nothing is written: every diagnostic is
// printed as serve prints it, and a build already there is left as it was.
func TestBuildRefusesAPageWithADiagnostic(t *testing.T) {
	files := buildSite()
	files["src/pages/broken.html"] = "<h1>{nope}</h1>\n"
	files["dist/old.txt"] = "the last build"
	dir := tempSite(t, files)
	stdout, stderr, code := runBuild(t, fakeTailwind(t, ""), dir)
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !regexp.MustCompile(`src/pages/broken\.html:1:\d+  .*nope`).MatchString(stderr) {
		t.Errorf("stderr does not show the diagnostic as file:line:col  message:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Nothing was built") {
		t.Errorf("stderr does not say nothing was built:\n%s", stderr)
	}
	if got := tree(t, filepath.Join(dir, "dist")); got != "old.txt" {
		t.Errorf("dist was changed:\n%s", got)
	}
}

// A sitemap needs the site's address. Without one there is none, robots.txt
// has no Sitemap line, and build says so. --url beats site.json.
func TestBuildSitemapNeedsTheSitesAddress(t *testing.T) {
	files := buildSite()
	files["src/data/site.json"] = `{"name": "No address"}`
	dir := tempSite(t, files)
	_, stderr, code := runBuild(t, fakeTailwind(t, ""), dir)
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "sitemap.xml")); err == nil {
		t.Error("a sitemap was written with no address to put in it")
	}
	if got := read(t, filepath.Join(dir, "dist", "robots.txt")); got != "User-agent: *\nAllow: /\n" {
		t.Errorf("robots.txt = %q", got)
	}
	if !strings.Contains(stderr, "no sitemap.xml") || !strings.Contains(stderr, "--url") {
		t.Errorf("stderr does not say why there is no sitemap:\n%s", stderr)
	}

	_, stderr, code = runBuild(t, fakeTailwind(t, ""), dir, "--url", "https://flag.example/")
	if code != 0 {
		t.Fatalf("--url: exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "dist", "sitemap.xml")), "<loc>https://flag.example/about</loc>") {
		t.Error("the sitemap does not use the address --url gave")
	}

	dir = tempSite(t, buildSite())
	if _, stderr, code := runBuild(t, fakeTailwind(t, ""), dir, "--url=https://flag.example"); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if sm := read(t, filepath.Join(dir, "dist", "sitemap.xml")); strings.Contains(sm, "example.com") {
		t.Error("site.json's address was used over --url")
	}
}

// A robots.txt or sitemap.xml the site keeps in public/ is the one shipped.
func TestBuildKeepsTheSitesOwnRobotsAndSitemap(t *testing.T) {
	files := buildSite()
	files["public/robots.txt"] = "User-agent: *\nDisallow: /secret\n"
	files["public/sitemap.xml"] = "<urlset/>"
	dir := tempSite(t, files)
	if _, stderr, code := runBuild(t, fakeTailwind(t, ""), dir); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if got := read(t, filepath.Join(dir, "dist", "robots.txt")); got != files["public/robots.txt"] {
		t.Errorf("robots.txt = %q", got)
	}
	if got := read(t, filepath.Join(dir, "dist", "sitemap.xml")); got != "<urlset/>" {
		t.Errorf("sitemap.xml = %q", got)
	}
}

// A build replaces the last one in the site's folder. A folder elsewhere
// with something in it is never emptied, and the site's own folders are
// never written into.
func TestBuildOut(t *testing.T) {
	dir := tempSite(t, buildSite())
	tw := fakeTailwind(t, "")
	writeSite(t, dir, map[string]string{"dist/stale.html": "old"})
	if _, stderr, code := runBuild(t, tw, dir); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "stale.html")); err == nil {
		t.Error("a file from the last build is still in dist")
	}

	elsewhere := filepath.Join(t.TempDir(), "site-out")
	if _, stderr, code := runBuild(t, tw, dir, "--out", elsewhere); code != 0 {
		t.Fatalf("--out: exit %d (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "index.html")); err != nil {
		t.Errorf("--out was not written: %v", err)
	}

	full := t.TempDir()
	writeSite(t, full, map[string]string{"mine.txt": "keep"})
	_, stderr, code := runBuild(t, tw, dir, "--out", full)
	if code != 1 || !strings.Contains(stderr, "not empty") {
		t.Errorf("a full folder elsewhere: exit %d, stderr %s", code, stderr)
	}
	if got := tree(t, full); got != "mine.txt" {
		t.Errorf("the folder elsewhere was changed:\n%s", got)
	}

	for _, bad := range []string{dir, filepath.Join(dir, "src"), filepath.Join(dir, "public", "x"), filepath.Dir(dir)} {
		_, stderr, code := runBuild(t, tw, dir, "--out", bad)
		if code != 2 {
			t.Errorf("--out %s: exit %d, want 2 (stderr: %s)", bad, code, stderr)
		}
	}
}

// What the engine has to say that is not a problem (a site still in
// resources/) is printed once, however many pages it applies to.
func TestBuildPrintsNoticesOnce(t *testing.T) {
	files := map[string]string{}
	for name, body := range buildSite() {
		if strings.HasPrefix(name, "src/") {
			name = "resources/" + strings.TrimPrefix(name, "src/")
			body = strings.ReplaceAll(body, "src/css", "resources/css")
		}
		files[name] = body
	}
	dir := tempSite(t, files)
	_, stderr, code := runBuild(t, fakeTailwind(t, ""), dir)
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if n := strings.Count(stderr, "resources/"); n != 1 {
		t.Errorf("the notice about resources/ was printed %d times:\n%s", n, stderr)
	}
}

func TestBuildUsage(t *testing.T) {
	dir := tempSite(t, buildSite())
	for _, args := range [][]string{
		{"build", dir, "--out"},
		{"build", dir, "--url"},
		{"build", dir, "--url", "example.com"},
		{"build", dir, "--force"},
		{"build", dir, "again"},
		{"build", filepath.Join(dir, "nope")},
	} {
		_, stderr, code := runCLI(t, "", args...)
		if code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q; want 2 and a message", args[1:], code, stderr)
		}
	}
	_, stderr, _ := runCLI(t, "")
	if !strings.Contains(stderr, "build [dir]") {
		t.Errorf("the usage text does not name build:\n%s", stderr)
	}
}

// A folder with no pages in it is not a site to build: said, and nothing
// written, rather than an empty dist/ that looks like success.
func TestBuildRefusesAFolderWithNoPages(t *testing.T) {
	dir := tempSite(t, map[string]string{"public/favicon.svg": "<svg/>"})
	_, stderr, code := runBuild(t, fakeTailwind(t, ""), dir)
	if code != 1 || !strings.Contains(stderr, "no pages") {
		t.Errorf("exit %d, stderr %s; want 1 and a message about no pages", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist")); err == nil {
		t.Error("dist was written")
	}
}
