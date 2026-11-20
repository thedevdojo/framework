package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/protocol"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

const serveUsage = "usage: devdojo serve [dir] [--port N] [--no-reload]"

// defaultServePort is where a site is served when --port is not given.
const defaultServePort = 8000

// portTries is how many ports after the default are tried when it is taken.
const portTries = 100

// watchInterval is how often the site folder is looked at for changes. Short
// enough that a save feels instant, long enough that walking the folder costs
// nothing anyone would notice.
const watchInterval = 300 * time.Millisecond

// reloadPath is the address a served page listens on for "a file changed". It
// is under a name no site would use for a page.
const reloadPath = "/__devdojo/reload"

// reloadScript is what every HTML response carries when live reload is on.
// EventSource reconnects by itself, so a restarted server is picked up again
// without anything more here.
const reloadScript = `<script>new EventSource("` + reloadPath + `").onmessage=function(){location.reload()}</script>`

// cmdServe serves the site in a folder on this machine, for the person
// writing it: every page is compiled when it is asked for, from the files as
// they are on disk at that moment, and the browser reloads when one changes.
// It never returns while the server is up.
func cmdServe(args []string) int {
	dir := ""
	port, explicit := defaultServePort, false
	reload := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value, isPort := "", false
		switch {
		case arg == "--no-reload":
			reload = false
		case arg == "--port":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "devdojo serve: --port needs a number\n"+serveUsage)
				return 2
			}
			i++
			value, isPort = args[i], true
		case strings.HasPrefix(arg, "--port="):
			value, isPort = strings.TrimPrefix(arg, "--port="), true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "devdojo serve: unknown option %q\n%s\n", arg, serveUsage)
			return 2
		case dir == "":
			dir = arg
		default:
			fmt.Fprintf(os.Stderr, "devdojo serve: one site at a time (got %q and %q)\n%s\n", dir, arg, serveUsage)
			return 2
		}
		if isPort {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 65535 {
				fmt.Fprintf(os.Stderr, "devdojo serve: --port needs a number from 1 to 65535, got %q\n%s\n", value, serveUsage)
				return 2
			}
			port, explicit = n, true
		}
	}
	if dir == "" {
		dir = "."
	}
	// A nonexistent or non-directory argument is a usage error, as it is for
	// lint: without this a typo'd path would serve a 404 for every address.
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo serve: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "devdojo serve: %q is not a directory\n", dir)
		return 2
	}

	listener, got, err := openListener(port, explicit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo serve: %v\n", err)
		return 1
	}
	if got != port {
		fmt.Fprintf(os.Stderr, "devdojo: port %d is in use, using %d instead\n", port, got)
	}

	handler := newServeHandler(dir, serveOptions{reload: reload})
	if reload {
		go handler.watch(watchInterval, nil)
	}
	// The one line on stdout, printed only once the port is really open, so a
	// script waiting for it can go on as soon as it appears.
	fmt.Printf("Serving %s at http://localhost:%d (Ctrl+C to stop)\n", dir, got)

	// No write timeout: the reload connection stays open for as long as the
	// page does.
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "devdojo serve: %v\n", err)
		return 1
	}
	return 0
}

// openListener opens the port the site is served on, on this machine only.
// A port the user asked for is used or it is an error. The default port is a
// guess, so when it is taken the next free one is used instead; the caller
// compares the port returned with the one asked for to say so.
func openListener(port int, explicit bool) (net.Listener, int, error) {
	if explicit {
		l, err := listenLocal(port)
		if err != nil {
			return nil, 0, fmt.Errorf("port %d is already in use: stop what is running there, or pick another with --port", port)
		}
		return l, port, nil
	}
	last := port + portTries
	if last > 65535 {
		last = 65535
	}
	for p := port; p <= last; p++ {
		if l, err := listenLocal(p); err == nil {
			return l, p, nil
		}
	}
	return nil, 0, fmt.Errorf("no free port from %d to %d: pick one with --port", port, last)
}

// listenLocal listens on one port of this machine's own address.
//
// Opening the port is not enough to know it is free. Another server may hold
// the same number on every address, or only on the IPv6 one, and this would
// still open beside it; the browser, asked for localhost, could then reach
// the other server. So the port is knocked on first, on both addresses.
func listenLocal(port int) (net.Listener, error) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			conn.Close()
			return nil, fmt.Errorf("something is already listening on %s", addr)
		}
	}
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// serveOptions is what `devdojo serve` can be told, apart from where the site
// is and which port to use.
type serveOptions struct {
	// reload puts the reload script in every HTML response and answers at
	// reloadPath. --no-reload turns it off.
	reload bool
	// stderr is the terminal: diagnostics and notices go here. os.Stderr when
	// nil; a test gives a buffer.
	stderr io.Writer
}

// siteServer answers every request for one site folder. It keeps nothing of
// the site between requests: the files are read again each time, which is
// what makes an edit show on reload.
type siteServer struct {
	dir     string
	reload  bool
	reloads *reloadHub

	mu   sync.Mutex      // guards term and said, as requests come in together
	term io.Writer       // the terminal
	said map[string]bool // lines that are printed once and not again
}

// newServeHandler builds the handler for the site in dir. It is apart from
// cmdServe so a test can ask it for pages without a port or a process.
func newServeHandler(dir string, opts serveOptions) *siteServer {
	term := opts.stderr
	if term == nil {
		term = os.Stderr
	}
	return &siteServer{
		dir:     dir,
		reload:  opts.reload,
		reloads: newReloadHub(),
		term:    term,
		said:    map[string]bool{},
	}
}

func (s *siteServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "devdojo serve only answers GET and HEAD", http.StatusMethodNotAllowed)
		return
	}
	urlPath, safe := cleanURLPath(r.URL.Path)
	if safe {
		if s.reload && urlPath == reloadPath {
			s.serveReloadEvents(w, r)
			return
		}
		// public/ before pages: a file there is served at its own address,
		// exactly as the site's host serves it.
		if file, ok := s.publicFile(urlPath); ok {
			servePublicFile(w, r, file)
			return
		}
	}
	s.servePage(w, urlPath, safe)
}

// cleanURLPath tidies a request's path (`//about/./` is `/about`) and says
// whether it may be looked up at all. A path with a `..` in it, a backslash
// (a `..` in disguise on some systems) or a NUL is never resolved against the
// disk, in public/ or in the pages: it gets the 404 page.
func cleanURLPath(p string) (string, bool) {
	if strings.ContainsAny(p, "\\\x00") {
		return p, false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return p, false
		}
	}
	return path.Clean("/" + p), true
}

// publicFile is the file in public/ that a URL names, when there is one.
// Only a file: `/` and `/images` are left for the pages to answer.
func (s *siteServer) publicFile(urlPath string) (string, bool) {
	rel := strings.TrimPrefix(urlPath, "/")
	if rel == "" {
		return "", false
	}
	root := filepath.Join(s.dir, "public")
	full := filepath.Join(root, filepath.FromSlash(rel))
	// cleanURLPath has already refused `..`; this is the same promise kept a
	// second way, so no later change to one can let a URL out of public/.
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return full, true
}

// contentTypes are the types of the files a site keeps in public/. They are
// spelled out so the answer is the same on every machine: the standard
// library otherwise asks the operating system, and each one says something a
// little different (or nothing) for .js, .svg and the font types.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".json": "application/json", ".map": "application/json", ".webmanifest": "application/manifest+json",
	".xml": "application/xml", ".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".svg": "image/svg+xml", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg", ".wav": "audio/wav",
	".pdf": "application/pdf", ".wasm": "application/wasm",
}

// servePublicFile sends one file from public/ as it is.
func servePublicFile(w http.ResponseWriter, r *http.Request, file string) {
	f, err := os.Open(file)
	if err != nil {
		http.Error(w, "404 Not Found", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "404 Not Found", http.StatusNotFound)
		return
	}
	ext := strings.ToLower(filepath.Ext(file))
	if ctype, ok := contentTypes[ext]; ok {
		w.Header().Set("Content-Type", ctype)
	} else if ctype := mime.TypeByExtension(ext); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	// The browser may keep its copy but has to ask each time whether it is
	// still the newest, so a changed image shows on the next reload.
	w.Header().Set("Cache-Control", "no-cache")
	// ServeContent answers range and if-modified-since requests, and works
	// the type out from the bytes when the extension said nothing.
	http.ServeContent(w, r, filepath.Base(file), info.ModTime(), f)
}

// pageTarget is one page to compile: the file, and for a dynamic page the
// data bound for this one render.
type pageTarget struct {
	entry string
	data  map[string]any
}

// servePage compiles the one page a URL lands on and sends it. Only that
// page is compiled, never the site, so the wait is the same on a site of
// three pages and of three hundred.
func (s *siteServer) servePage(w http.ResponseWriter, urlPath string, safe bool) {
	files := vfs.Dir(s.dir)
	target, status, found := resolvePage(files, urlPath, safe)
	if !found {
		s.sendHTML(w, http.StatusNotFound, plainNotFound(urlPath))
		return
	}

	result := render.Compile(files, []string{target.entry}, render.Options{Data: target.data})
	for _, n := range result.Notices {
		s.sayOnce("devdojo: " + n.Message)
	}

	diags := append([]render.Diagnostic(nil), result.Diagnostics...)
	sort.SliceStable(diags, func(i, j int) bool {
		a, b := diags[i], diags[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	var failures []string
	for _, d := range diags {
		if d.Kind == render.KindError {
			failures = append(failures, diagnosticLine(d))
			continue
		}
		// Anything else (app-tier: Blade that needs real PHP) leaves the page
		// standing. It is said once, not on every reload of every page that
		// shares the layout it is in.
		s.sayOnce(diagnosticLine(d))
	}
	body, rendered := result.Files[site.OutputName(target.entry)]
	if len(failures) == 0 && !rendered {
		// Not reachable while the engine reports every page it cannot make,
		// but a blank 200 would be the worst answer if that ever changes.
		failures = append(failures, target.entry+":0:0  the page could not be compiled")
	}
	if len(failures) > 0 {
		// An error is printed every time: the terminal then shows whether the
		// last save fixed it.
		s.say(strings.Join(failures, "\n"))
		s.sendHTML(w, http.StatusInternalServerError, errorPage(target.entry, failures))
		return
	}
	s.sendHTML(w, status, body)
}

// diagnosticLine is one diagnostic as the terminal and the error page show
// it: `file:line:col  message`, the message as `devdojo lint` words it. A
// kind that does not fail the page is named, so it is not read as an error.
func diagnosticLine(d render.Diagnostic) string {
	msg := d.Message
	if d.Construct != "" {
		msg = d.Construct + ": " + msg
	}
	if d.Kind != render.KindError {
		msg = "[" + string(d.Kind) + "] " + msg
	}
	return fmt.Sprintf("%s:%d:%d  %s", d.File, d.Line, d.Col, msg)
}

// sendHTML writes an HTML response that is never kept by the browser: the
// next request must compile the page again.
func (s *siteServer) sendHTML(w http.ResponseWriter, status int, body string) {
	if s.reload {
		body = withReloadScript(body)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

// withReloadScript puts the reload script just before the page's last
// </body>, or at the very end of a page that has none.
func withReloadScript(page string) string {
	const closing = "</body>"
	for i := len(page) - len(closing); i >= 0; i-- {
		if strings.EqualFold(page[i:i+len(closing)], closing) {
			return page[:i] + reloadScript + page[i:]
		}
	}
	return page + reloadScript
}

// errorPage is what a page that does not compile is answered with: every
// reason, where it is, readable without opening the terminal.
func errorPage(entry string, lines []string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>This page did not compile</title></head>` +
		`<body style="margin:0;padding:3rem;background:#111;color:#eee;font:15px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace">` +
		`<h1 style="margin:0 0 .5rem;font-size:1.25rem">This page did not compile</h1>` +
		`<p style="margin:0 0 1.5rem;color:#999">`)
	b.WriteString(html.EscapeString(entry))
	b.WriteString(`</p><pre style="margin:0;white-space:pre-wrap;color:#ff7b72">`)
	for _, line := range lines {
		b.WriteString(html.EscapeString(line))
		b.WriteString("\n")
	}
	b.WriteString(`</pre></body></html>`)
	return b.String()
}

// plainNotFound is the answer for an address nothing serves on a site with
// no 404 page of its own.
func plainNotFound(urlPath string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>404 Not Found</title></head>` +
		`<body><h1>404 Not Found</h1><p>No page at ` + html.EscapeString(urlPath) + `</p></body></html>`
}

// say prints to the terminal, one whole message at a time.
func (s *siteServer) say(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintln(s.term, text)
}

// sayOnce prints a line the first time it is given and never again.
func (s *siteServer) sayOnce(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.said[line] {
		return
	}
	s.said[line] = true
	fmt.Fprintln(s.term, line)
}

// servePageExts are the two page extensions, Blade first: where one address
// has a page in each syntax the Blade one serves it (protocol/routing.go).
var servePageExts = []string{".blade.php", ".html"}

// resolvePage finds the page for a URL, in the order the platform serves a
// published site: a static page, then a dynamic one, then the site's 404
// page with status 404. found is false when there is not even a 404 page.
func resolvePage(files vfs.FS, urlPath string, safe bool) (target pageTarget, status int, found bool) {
	layout := site.Detect(files)
	notFound := ""
	for _, ext := range servePageExts {
		if _, ok := files.Read(layout.Pages + "404" + ext); ok {
			notFound = layout.Pages + "404" + ext
			break
		}
	}
	if safe {
		// ResolveURL answers with the 404 page for an address it has no page
		// for. That is only a hit when the 404 page is what was asked for. A
		// dynamic page's own file name is not an address either: compiled
		// with no entry bound it could only fail.
		entry, ok := protocol.ResolveURL(files, urlPath)
		direct := entry != notFound || strings.Trim(urlPath, "/") == "404"
		if _, _, dynamic := parseDynamicPage(entry); ok && direct && !dynamic {
			return pageTarget{entry: entry}, http.StatusOK, true
		}
		if target, ok := resolveDynamicPage(files, layout, urlPath); ok {
			return target, http.StatusOK, true
		}
	}
	if notFound == "" {
		return pageTarget{}, http.StatusNotFound, false
	}
	return pageTarget{entry: notFound}, http.StatusNotFound, true
}

// dynamicPageName is the `[collection.field]` file name of a dynamic page.
// Both halves follow the variable-name rule the engine applies to a
// collection's name. It is the platform's own pattern (DynamicPages.php).
var dynamicPageName = regexp.MustCompile(`^\[([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\]$`)

// parseDynamicPage reads the collection and field out of a dynamic page's
// path, e.g. src/pages/blog/[posts.slug].html → posts, slug.
func parseDynamicPage(pagePath string) (collection, field string, ok bool) {
	base := path.Base(pagePath)
	for _, ext := range servePageExts {
		if !strings.HasSuffix(base, ext) {
			continue
		}
		if m := dynamicPageName.FindStringSubmatch(strings.TrimSuffix(base, ext)); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// resolveDynamicPage finds the dynamic page and the collection entry a URL
// names. The URL's last segment is the value looked for; the segments before
// it name the folder that holds the page. The first dynamic page in that
// folder, in name order, whose collection has a matching entry serves it.
//
// This is the platform's lookup (DynamicPages::resolveUrl) and binds what the
// platform binds (FrameworkCompiler::dynamicResult): the collection's name is
// the ONE matched entry for this render, and `entries` is the whole
// collection, for a page that also lists the others.
func resolveDynamicPage(files vfs.FS, layout site.Layout, urlPath string) (pageTarget, bool) {
	p := strings.Trim(urlPath, "/")
	if p == "" || strings.Contains(p, "..") {
		return pageTarget{}, false
	}
	cut := strings.LastIndex(p, "/") + 1
	directory, segment := layout.Pages+p[:cut], p[cut:]

	for _, candidate := range files.List(directory) {
		// Direct children only: a dynamic page in a folder further down
		// serves its own, deeper addresses.
		if path.Dir(candidate)+"/" != directory {
			continue
		}
		collection, field, ok := parseDynamicPage(candidate)
		if !ok {
			continue
		}
		// A Studio page with a Blade twin beside it is never served.
		if strings.HasSuffix(candidate, ".html") {
			if _, twin := files.Read(strings.TrimSuffix(candidate, ".html") + ".blade.php"); twin {
				continue
			}
		}
		entries := collectionEntries(files, layout, collection)
		for _, entry := range entries {
			if value, ok := scalarString(entry.(map[string]any)[field]); ok && value == segment {
				return pageTarget{entry: candidate, data: map[string]any{collection: entry, "entries": entries}}, true
			}
		}
	}
	return pageTarget{}, false
}

// collectionEntries is a collection's entries: data/collections/<name>.json
// read as a JSON array, keeping the objects in it. A file that is missing,
// malformed or not an array has none; the engine says what is wrong with it
// when a page reads it. Every element is a map[string]any; the slice is
// []any because that is the shape render.Options.Data takes.
func collectionEntries(files vfs.FS, layout site.Layout, name string) []any {
	raw, ok := files.Read(layout.Collections() + name + ".json")
	if !ok {
		return nil
	}
	var list []any
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	entries := make([]any, 0, len(list))
	for _, item := range list {
		if _, isObject := item.(map[string]any); isObject {
			entries = append(entries, item)
		}
	}
	return entries
}

// scalarString is a field's value as the text a URL would carry, the way the
// platform compares them (PHP's string cast): a number as its digits, true as
// "1". Anything that is not a single value never matches an address.
func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		if x {
			return "1", true
		}
		return "", true
	}
	return "", false
}

// reloadHub is the list of browsers waiting to be told a file changed.
type reloadHub struct {
	mu      sync.Mutex
	waiting map[chan struct{}]struct{}
}

func newReloadHub() *reloadHub {
	return &reloadHub{waiting: map[chan struct{}]struct{}{}}
}

// join adds one browser to the list. The channel gets a value for every
// change; leave takes the browser off the list again.
func (h *reloadHub) join() (events <-chan struct{}, leave func()) {
	// Room for one: a change that arrives while the last one is still being
	// written is the same news, and the sender never waits on a slow browser.
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.waiting[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.waiting, ch)
		h.mu.Unlock()
	}
}

// broadcast tells every browser on the list to reload.
func (h *reloadHub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.waiting {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// serveReloadEvents is the open connection behind the reload script: a
// server-sent event stream that says nothing until a file changes.
func (s *siteServer) serveReloadEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "live reload needs a connection that can stream", http.StatusInternalServerError)
		return
	}
	// Joined before the first byte is sent, so a change made the moment the
	// browser sees the connection open is not missed.
	events, leave := s.reloads.join()
	defer leave()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-events:
			io.WriteString(w, "data: reload\n\n")
			flusher.Flush()
		}
	}
}

// watch looks at the site folder every interval and tells the browsers when
// anything in it is different from the last look. It polls modification
// times because that needs nothing outside the standard library and behaves
// the same on every system. It runs until stop is closed; nil never stops it.
func (s *siteServer) watch(interval time.Duration, stop <-chan struct{}) {
	last := siteSnapshot(s.dir)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			now := siteSnapshot(s.dir)
			if snapshotsDiffer(last, now) {
				s.reloads.broadcast()
			}
			last = now
		}
	}
}

// fileStamp is what is remembered of a file between two looks. The size is
// kept beside the time because two saves can land inside one tick of a file
// system's clock.
type fileStamp struct {
	modified int64 // nanoseconds
	size     int64
}

// watchedFolders are the folders of a site whose files end up in a page: the
// source (under either of its names) and public/.
var watchedFolders = map[string]bool{"src": true, "resources": true, "public": true}

// unwatchedFolders are never looked into, wherever they are: they are large,
// and nothing in them is the site's source.
var unwatchedFolders = map[string]bool{".git": true, "node_modules": true, "dist": true}

// siteSnapshot is every watched file of the site in dir with its stamp: all
// of src/, resources/ and public/, and the files directly in dir. Other
// folders beside them are not the site's and are left alone. Paths are
// forward-slash, relative to dir. A file that cannot be read is left out,
// which reads as "removed" until it can be again.
func siteSnapshot(dir string) map[string]fileStamp {
	snap := map[string]fileStamp{}
	add := func(full string) {
		// Stat, not the walk's own entry: a link is as new as what it points to.
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		rel, err := filepath.Rel(dir, full)
		if err != nil {
			return
		}
		snap[filepath.ToSlash(rel)] = fileStamp{modified: info.ModTime().UnixNano(), size: info.Size()}
	}

	top, err := os.ReadDir(dir)
	if err != nil {
		return snap
	}
	for _, entry := range top {
		full := filepath.Join(dir, entry.Name())
		if !entry.IsDir() {
			// The Finder rewrites this file when a folder is only looked at.
			if entry.Name() != ".DS_Store" {
				add(full)
			}
			continue
		}
		if !watchedFolders[entry.Name()] {
			continue
		}
		filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if unwatchedFolders[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() != ".DS_Store" {
				add(p)
			}
			return nil
		})
	}
	return snap
}

// snapshotsDiffer reports whether a file was added, removed or changed
// between two looks.
func snapshotsDiffer(a, b map[string]fileStamp) bool {
	if len(a) != len(b) {
		return true
	}
	for name, stamp := range a {
		if other, ok := b[name]; !ok || other != stamp {
			return true
		}
	}
	return false
}
