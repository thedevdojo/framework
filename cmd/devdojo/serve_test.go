// These tests are in package main, not main_test like the rest of the CLI's:
// `serve` never exits by itself, so it is tested through the handler it
// builds (net/http/httptest) and the small functions around it, against sites
// written into a temp dir. Nothing here starts the built binary.
package main

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeSite writes files (forward-slash paths → contents) under a fresh temp
// dir and returns it.
func writeSite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		writeFile(t, dir, name, body)
	}
	return dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lockedBuffer is a terminal the handler can write to from any request.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// quietHandler is the handler for dir with live reload off, so a body can be
// compared to what the page renders to, and with its terminal in the returned
// buffer.
func quietHandler(dir string) (http.Handler, *lockedBuffer) {
	term := &lockedBuffer{}
	return newServeHandler(dir, serveOptions{reload: false, stderr: term}), term
}

// get asks h for target. The URL is put on the request as it is, with no
// cleaning, so a test can send `..`.
func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestServeIndexPage(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{
		"src/pages/index.html": "<h1>Home</h1>",
	}))
	rec := get(t, h, "/")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	if got := rec.Body.String(); got != "<h1>Home</h1>" {
		t.Errorf("body = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestServeNestedPages(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{
		"src/pages/index.html":           "home",
		"src/pages/about.html":           "flat about",
		"src/pages/about/index.html":     "nested about",
		"src/pages/blog/index.html":      "blog index",
		"src/pages/docs/intro.blade.php": "docs {{ 'intro' }}",
	}))
	for target, want := range map[string]string{
		"/about":      "flat about", // a flat file beats a directory index
		"/blog":       "blog index",
		"/docs/intro": "docs intro",
	} {
		rec := get(t, h, target)
		if rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("GET %s = %d %q, want 200 %q", target, rec.Code, rec.Body, want)
		}
	}
}

func TestServeTrailingSlash(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{
		"src/pages/about.html":      "about",
		"src/pages/blog/index.html": "blog",
	}))
	for target, want := range map[string]string{"/about/": "about", "/blog/": "blog"} {
		rec := get(t, h, target)
		if rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("GET %s = %d %q, want 200 %q", target, rec.Code, rec.Body, want)
		}
	}
}

func TestServePublicFiles(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{
		"src/pages/index.html":   "home",
		"src/pages/about.html":   "the page",
		"public/images/a.png":    "\x89PNG not really",
		"public/robots.txt":      "User-agent: *",
		"public/js/app.js":       "console.log(1)",
		"public/styles/site.css": "body{}",
		"public/favicon.svg":     "<svg/>",
		"public/fonts/a.woff2":   "font",
		"public/about":           "the public file",
	}))
	for target, wantType := range map[string]string{
		"/images/a.png":    "image/png",
		"/robots.txt":      "text/plain; charset=utf-8",
		"/js/app.js":       "text/javascript; charset=utf-8",
		"/styles/site.css": "text/css; charset=utf-8",
		"/favicon.svg":     "image/svg+xml",
		"/fonts/a.woff2":   "font/woff2",
	} {
		rec := get(t, h, target)
		if rec.Code != 200 {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != wantType {
			t.Errorf("GET %s Content-Type = %q, want %q", target, got, wantType)
		}
	}
	if got := get(t, h, "/images/a.png").Body.String(); got != "\x89PNG not really" {
		t.Errorf("public file body = %q", got)
	}
	// public/ is looked in before the pages.
	if got := get(t, h, "/about").Body.String(); got != "the public file" {
		t.Errorf("GET /about = %q, want the file in public/", got)
	}
	// A folder in public/ is not a file: the page still serves.
	if got := get(t, h, "/").Body.String(); got != "home" {
		t.Errorf("GET / = %q, want the index page", got)
	}
}

func TestServeRefusesTraversal(t *testing.T) {
	dir := writeSite(t, map[string]string{
		"src/pages/index.html": "home",
		"src/data/site.json":   `{"name":"TOPSECRET"}`,
		"secret.txt":           "TOPSECRET",
		"public/ok.txt":        "ok",
	})
	h, _ := quietHandler(dir)
	for _, target := range []string{
		"/../secret.txt",
		"/x/../../secret.txt",
		"/%2e%2e/secret.txt",
		"/..%2fsecret.txt",
		"/../src/data/site.json",
		"/images/..%5c..%5csecret.txt",
	} {
		rec := get(t, h, target)
		if rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "TOPSECRET") {
			t.Errorf("GET %s leaked a file outside public/: %q", target, rec.Body)
		}
	}
}

// dynamicSite has one page per post, a list of every post beside the matched
// one, and a 404 page.
func dynamicSite(t *testing.T) string {
	return writeSite(t, map[string]string{
		"src/pages/index.html":             "home",
		"src/pages/404.html":               "<h1>Lost</h1>",
		"src/pages/blog/index.html":        "all posts",
		"src/pages/blog/[posts.slug].html": "<h1>{{ posts.title }}</h1><Foreach entries=\"e\">[{{ e.slug }}]</Foreach>",
		"src/pages/blog/pinned.html":       "the static page",
		"src/data/collections/posts.json": `[
			{"slug": "hello", "title": "Hello there"},
			{"slug": "pinned", "title": "Pinned post"},
			{"slug": "second", "title": "Second post"},
			{"slug": 42, "title": "A number"},
			"not an entry"
		]`,
	})
}

func TestServeDynamicPage(t *testing.T) {
	h, _ := quietHandler(dynamicSite(t))

	rec := get(t, h, "/blog/hello")
	if rec.Code != 200 {
		t.Fatalf("GET /blog/hello = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	// The collection's name is the matched entry; `entries` is every entry.
	if want := "<h1>Hello there</h1>[hello][pinned][second][42]"; rec.Body.String() != want {
		t.Errorf("GET /blog/hello = %q, want %q", rec.Body, want)
	}
	if got := get(t, h, "/blog/second/").Body.String(); !strings.Contains(got, "<h1>Second post</h1>") {
		t.Errorf("GET /blog/second/ = %q", got)
	}
	// A field that is a number matches the address written the same way.
	if got := get(t, h, "/blog/42").Body.String(); !strings.Contains(got, "<h1>A number</h1>") {
		t.Errorf("GET /blog/42 = %q", got)
	}
	// A static page beats the dynamic one for the same address.
	if got := get(t, h, "/blog/pinned").Body.String(); got != "the static page" {
		t.Errorf("GET /blog/pinned = %q, want the static page", got)
	}
}

func TestServeDynamicPageMiss(t *testing.T) {
	h, _ := quietHandler(dynamicSite(t))
	for _, target := range []string{"/blog/nope", "/blog/hello/more", "/blog/[posts.slug]"} {
		rec := get(t, h, target)
		if rec.Code != 404 || rec.Body.String() != "<h1>Lost</h1>" {
			t.Errorf("GET %s = %d %q, want the 404 page", target, rec.Code, rec.Body)
		}
	}
}

func TestServeDynamicBladePage(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{
		"src/pages/[team.id].blade.php":  "<p>{{ $team->name }}</p>",
		"src/pages/[team.id].html":       "never served: the Blade page beside it answers",
		"src/data/collections/team.json": `[{"id": "ada", "name": "Ada"}]`,
	}))
	rec := get(t, h, "/ada")
	if rec.Code != 200 || rec.Body.String() != "<p>Ada</p>" {
		t.Errorf("GET /ada = %d %q", rec.Code, rec.Body)
	}
}

func TestServeNotFound(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{
		"src/pages/index.html": "home",
		"src/pages/404.html":   "<h1>Lost</h1>",
	}))
	rec := get(t, h, "/nope")
	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if rec.Body.String() != "<h1>Lost</h1>" {
		t.Errorf("body = %q, want the site's 404 page", rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	// With no 404 page there is still an answer, and it is still a 404.
	h, _ = quietHandler(writeSite(t, map[string]string{"src/pages/index.html": "home"}))
	rec = get(t, h, "/nope")
	if rec.Code != 404 {
		t.Errorf("status with no 404 page = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "404") {
		t.Errorf("body with no 404 page = %q", rec.Body)
	}
}

func TestServeErrorPage(t *testing.T) {
	h, term := quietHandler(writeSite(t, map[string]string{
		"src/pages/index.html": "<h1>ok</h1>\n  {{ nope }}\n<Foreach items=\"<b>\">x</Foreach>",
	}))
	rec := get(t, h, "/")
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500 (body: %s)", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body := rec.Body.String()
	line := "src/pages/index.html:2:3  undefined-variable: undefined variable nope"
	if !strings.Contains(body, line) {
		t.Errorf("error page should list %q, got:\n%s", line, body)
	}
	if !strings.Contains(term.String(), line+"\n") {
		t.Errorf("terminal should print %q, got:\n%s", line, term.String())
	}
	// Whatever a diagnostic quotes from the page is text on the error page.
	if strings.Contains(body, "<Foreach>") || !strings.Contains(body, "&lt;Foreach&gt;") {
		t.Errorf("diagnostics should be HTML-escaped, got:\n%s", body)
	}
	if !strings.Contains(term.String(), "<Foreach>") {
		t.Errorf("the terminal gets the line as it is, got:\n%s", term.String())
	}
}

func TestServeAppTierDoesNotFailThePage(t *testing.T) {
	h, term := quietHandler(writeSite(t, map[string]string{
		"src/pages/index.blade.php": "@php $x = 1; @endphp\n<p>hi</p>",
	}))
	rec := get(t, h, "/")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "app tier") {
		t.Errorf("an app-tier diagnostic belongs in the terminal, not the page: %q", rec.Body)
	}
	if !strings.Contains(term.String(), "src/pages/index.blade.php:1:1  ") || !strings.Contains(term.String(), "app tier") {
		t.Errorf("terminal should print the app-tier diagnostic, got:\n%s", term.String())
	}
	// Once is enough: the same line is not printed again on every reload.
	before := term.String()
	get(t, h, "/")
	if term.String() != before {
		t.Errorf("an app-tier line was printed twice:\n%s", term.String())
	}
}

func TestServeEditShowsOnReload(t *testing.T) {
	dir := writeSite(t, map[string]string{
		"src/pages/index.html":             "<Layout src=\"main.html\">before</Layout>",
		"src/components/layouts/main.html": "<main><Slot /></main>",
	})
	h, _ := quietHandler(dir)
	if got := get(t, h, "/").Body.String(); !strings.Contains(got, "<main>before</main>") {
		t.Fatalf("first render = %q", got)
	}
	writeFile(t, dir, "src/pages/index.html", "<Layout src=\"main.html\">after</Layout>")
	writeFile(t, dir, "src/components/layouts/main.html", "<article><Slot /></article>")
	writeFile(t, dir, "src/pages/new.html", "a new page")
	if got := get(t, h, "/").Body.String(); !strings.Contains(got, "<article>after</article>") {
		t.Errorf("after an edit = %q, want the new text", got)
	}
	if rec := get(t, h, "/new"); rec.Code != 200 || rec.Body.String() != "a new page" {
		t.Errorf("a page added while serving = %d %q", rec.Code, rec.Body)
	}
}

func TestServeNoticesOncePerMessage(t *testing.T) {
	h, term := quietHandler(writeSite(t, map[string]string{
		"resources/views/pages/index.blade.php": "<h1>Old shape</h1>",
		"resources/views/pages/about.blade.php": "about",
	}))
	for _, target := range []string{"/", "/about", "/"} {
		rec := get(t, h, target)
		if rec.Code != 200 {
			t.Fatalf("GET %s = %d", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "resources/") {
			t.Errorf("a notice reached the page: %q", rec.Body)
		}
	}
	if n := strings.Count(term.String(), "devdojo: This site keeps its source in resources/."); n != 1 {
		t.Errorf("the resources/ notice was printed %d times, want 1:\n%s", n, term.String())
	}
}

func TestServeReloadScript(t *testing.T) {
	dir := writeSite(t, map[string]string{
		"src/pages/index.html": "<html><body><p>one</p></body><!-- </body> --></BODY></html>",
		"src/pages/bare.html":  "<p>no body tag</p>",
		"src/pages/bad.html":   "{{ nope }}",
		"src/pages/404.html":   "<body>lost</body>",
		"public/page.html":     "<body>a file, served as it is</body>",
	})
	term := &lockedBuffer{}
	h := newServeHandler(dir, serveOptions{reload: true, stderr: term})

	// Before the LAST </body>, however it is written.
	body := get(t, h, "/").Body.String()
	if !strings.HasSuffix(body, "</script></BODY></html>") {
		t.Errorf("script should sit just before the last </body>, got %q", body)
	}
	if strings.Count(body, "<script>") != 1 || !strings.Contains(body, `new EventSource("/__devdojo/reload")`) || !strings.Contains(body, "location.reload()") {
		t.Errorf("reload script missing from %q", body)
	}
	// Appended when the page has no </body>.
	if body := get(t, h, "/bare").Body.String(); !strings.HasPrefix(body, "<p>no body tag</p>") || !strings.HasSuffix(body, "</script>") {
		t.Errorf("script should be appended, got %q", body)
	}
	// The error page and the 404 page reload too: that is how a fix shows.
	if rec := get(t, h, "/bad"); rec.Code != 500 || !strings.Contains(rec.Body.String(), "/__devdojo/reload") {
		t.Errorf("error page should carry the script: %d %q", rec.Code, rec.Body)
	}
	if rec := get(t, h, "/nope"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "/__devdojo/reload") {
		t.Errorf("404 page should carry the script: %d %q", rec.Code, rec.Body)
	}
	// A file in public/ is the site's own bytes.
	if body := get(t, h, "/page.html").Body.String(); body != "<body>a file, served as it is</body>" {
		t.Errorf("a public file was changed: %q", body)
	}

	// --no-reload: no script anywhere, and nothing at the events address.
	h = newServeHandler(dir, serveOptions{reload: false, stderr: term})
	for _, target := range []string{"/", "/bare", "/bad", "/nope"} {
		if body := get(t, h, target).Body.String(); strings.Contains(body, "__devdojo") || strings.Contains(body, "EventSource") {
			t.Errorf("GET %s with reload off still has the script: %q", target, body)
		}
	}
	if rec := get(t, h, "/__devdojo/reload"); rec.Code != 404 {
		t.Errorf("GET /__devdojo/reload with reload off = %d, want 404", rec.Code)
	}
}

// TestServeReloadEvents connects the way a browser does and waits for the
// message a change sends.
func TestServeReloadEvents(t *testing.T) {
	dir := writeSite(t, map[string]string{"src/pages/index.html": "home"})
	h := newServeHandler(dir, serveOptions{reload: true, stderr: &lockedBuffer{}})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/__devdojo/reload", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	// The response headers are back, so this browser is on the list.
	h.reloads.broadcast()

	lines := bufio.NewScanner(resp.Body)
	for lines.Scan() {
		if strings.HasPrefix(lines.Text(), "data:") {
			return
		}
	}
	t.Fatalf("no reload message arrived: %v", lines.Err())
}

// TestServeWatchSendsReload runs the real watcher against a real edit.
func TestServeWatchSendsReload(t *testing.T) {
	dir := writeSite(t, map[string]string{"src/pages/index.html": "home"})
	h := newServeHandler(dir, serveOptions{reload: true, stderr: &lockedBuffer{}})
	events, leave := h.reloads.join()
	defer leave()

	stop := make(chan struct{})
	defer close(stop)
	go h.watch(5*time.Millisecond, stop)

	// Give the watcher its first look, then change the site until it notices:
	// one write could land before that first look and so be the baseline.
	deadline := time.After(10 * time.Second)
	for i := 0; ; i++ {
		writeFile(t, dir, "src/pages/index.html", "edit "+strconv.Itoa(i))
		select {
		case <-events:
			return
		case <-deadline:
			t.Fatal("the watcher never sent a reload")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestSnapshotChanges(t *testing.T) {
	dir := writeSite(t, map[string]string{
		"src/pages/index.html":      "home",
		"src/components/nav.html":   "nav",
		"resources/css/site.css":    "body{}",
		"public/images/a.png":       "png",
		"template.json":             "{}",
		"node_modules/pkg/index.js": "x",
		".git/HEAD":                 "ref",
		"dist/index.html":           "built",
		"src/node_modules/x/y.js":   "x",
		"other/notes.txt":           "not part of the site",
	})
	old := time.Now().Add(-time.Hour)
	touch := func(name string, when time.Time) {
		t.Helper()
		if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(name)), when, when); err != nil {
			t.Fatal(err)
		}
	}

	base := siteSnapshot(dir)
	if snapshotsDiffer(base, siteSnapshot(dir)) {
		t.Fatal("two looks at an untouched site differ")
	}
	for _, name := range []string{"src/pages/index.html", "src/components/nav.html", "resources/css/site.css", "public/images/a.png", "template.json"} {
		if _, ok := base[name]; !ok {
			t.Errorf("snapshot should hold %s; it has %v", name, base)
		}
	}

	// Each change is made, seen, and then becomes the new baseline.
	step := func(what string, change func()) {
		t.Helper()
		change()
		next := siteSnapshot(dir)
		if !snapshotsDiffer(base, next) {
			t.Errorf("%s was not noticed", what)
		}
		base = next
	}
	step("a new modification time", func() { touch("src/pages/index.html", old) })
	step("a new file", func() { writeFile(t, dir, "src/pages/about.html", "about") })
	step("a removed file", func() { os.Remove(filepath.Join(dir, "src", "components", "nav.html")) })
	step("a changed public file", func() { touch("public/images/a.png", old) })
	step("a changed resources file", func() { touch("resources/css/site.css", old) })
	step("a changed root file", func() { touch("template.json", old) })
	step("a new root file", func() { writeFile(t, dir, "README.md", "hi") })
	// Same time, different length: an edit inside one tick of the clock.
	step("a new size", func() {
		writeFile(t, dir, "src/pages/about.html", "about, longer")
		touch("src/pages/about.html", old)
		base = siteSnapshot(dir)
		writeFile(t, dir, "src/pages/about.html", "short")
		touch("src/pages/about.html", old)
	})

	// What is not the site's source is not watched.
	for _, name := range []string{"node_modules/pkg/index.js", ".git/HEAD", "dist/index.html", "src/node_modules/x/y.js", "other/notes.txt"} {
		touch(name, old)
		writeFile(t, dir, name+".new", "x")
	}
	if snapshotsDiffer(base, siteSnapshot(dir)) {
		t.Errorf("a change under .git, node_modules, dist or an unwatched folder was noticed")
	}
}

// takenPort holds a port open for the length of the test.
func takenPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func TestServePortFallback(t *testing.T) {
	taken := takenPort(t)

	// The default port, taken: the next free one, and the caller is told.
	l, port, err := openListener(taken, false)
	if err != nil {
		t.Fatalf("a taken default port should fall back, got %v", err)
	}
	defer l.Close()
	if port <= taken {
		t.Errorf("fell back to %d, want a port after %d", port, taken)
	}
	if got := l.Addr().(*net.TCPAddr).Port; got != port {
		t.Errorf("listening on %d but reported %d", got, port)
	}

	// A port the user asked for, taken: an error that names it.
	if l, _, err := openListener(taken, true); err == nil {
		l.Close()
		t.Fatal("an explicit taken port should be an error")
	} else if !strings.Contains(err.Error(), strconv.Itoa(taken)) {
		t.Errorf("error should name the port: %v", err)
	}

	// A free port is used as asked, either way.
	l.Close()
	l2, got, err := openListener(port, true)
	if err != nil {
		t.Fatalf("a free port should open: %v", err)
	}
	defer l2.Close()
	if got != port {
		t.Errorf("opened %d, want %d", got, port)
	}
}

func TestServeUsageErrors(t *testing.T) {
	dir := writeSite(t, map[string]string{"src/pages/index.html": "home"})
	file := filepath.Join(dir, "src", "pages", "index.html")
	taken := strconv.Itoa(takenPort(t))
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"missing dir", []string{"serve", filepath.Join(dir, "nope")}, 2},
		{"a file, not a dir", []string{"serve", file}, 2},
		{"two dirs", []string{"serve", dir, dir}, 2},
		{"unknown option", []string{"serve", dir, "--bogus"}, 2},
		{"--port with no number", []string{"serve", dir, "--port"}, 2},
		{"--port not a number", []string{"serve", dir, "--port", "http"}, 2},
		{"--port out of range", []string{"serve", dir, "--port=70000"}, 2},
		{"explicit port taken", []string{"serve", dir, "--port", taken}, 1},
		{"explicit port taken, = form", []string{"serve", "--no-reload", "--port=" + taken, dir}, 1},
	} {
		if got := run(tc.args); got != tc.want {
			t.Errorf("%s: exit code = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestServeOnlyAnswersReads(t *testing.T) {
	h, _ := quietHandler(writeSite(t, map[string]string{"src/pages/index.html": "home"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x")))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d, want 405", rec.Code)
	}
}
