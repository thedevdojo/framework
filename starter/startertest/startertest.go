// Package startertest is what the tests of `devdojo new` share: a small site
// to stand in for a template, a way to pack files the way GitHub packs a
// repository's download, and a server that hands those downloads out. Nothing
// here reaches the network.
package startertest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

// Site is the smallest template worth the name: a page, its layout, the data
// and the stylesheet it reads, and a file in public/. Beside it are the files
// only a template listing needs, which `devdojo new` leaves out.
func Site() map[string]string {
	return map[string]string{
		"src/pages/index.html": "<Layout src=\"main.html\">\n<h1>{{ site.name }}</h1>\n</Layout>\n",
		"src/components/layouts/main.html": "<!doctype html>\n<html>\n<head>\n<title>{{ site.name }}</title>\n" +
			"<Vite src=\"src/css/site.css\" />\n</head>\n<body>\n<Slot />\n</body>\n</html>\n",
		"src/data/site.json":      `{"name": "Fixture"}`,
		"src/css/site.css":        "@theme { --color-brand: #123456; }\n",
		"public/favicon.svg":      `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		".gitignore":              "dist/\n",
		"template.json":           `{"name": "Fixture"}`,
		"thumbnail.png":           "not really a png",
		"README.md":               "# The fixture template\n",
		".github/workflows/x.yml": "on: push\n",
		".gitattributes":          "* text=auto\n",
		".DS_Store":               "finder",
		"src/.DS_Store":           "finder",
	}
}

// Entry is one entry of an archive, for the archives a test builds by hand:
// links, devices and names that climb out of the folder.
type Entry struct {
	Name     string
	Body     string
	Type     byte // tar.TypeReg when zero
	Linkname string
}

// Archive packs files the way GitHub's download of a repository is packed: a
// gzipped tar with a global header first, then everything inside one top
// folder named top (GitHub names it <repo>-<ref>).
func Archive(top string, files map[string]string, extra ...Entry) []byte {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := []Entry{{Name: "pax_global_header", Type: tar.TypeXGlobalHeader}, {Name: top + "/", Type: tar.TypeDir}}
	for _, name := range names {
		entries = append(entries, Entry{Name: top + "/" + name, Body: files[name]})
	}
	entries = append(entries, extra...)
	return Pack(entries...)
}

// Pack packs entries exactly as given, as a gzipped tar.
func Pack(entries ...Entry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.Type
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.Name, Typeflag: typ, Mode: 0o644, Linkname: e.Linkname}
		switch typ {
		case tar.TypeReg:
			hdr.Size = int64(len(e.Body))
		case tar.TypeDir:
			hdr.Mode = 0o755
		case tar.TypeXGlobalHeader:
			hdr = &tar.Header{Typeflag: typ, PAXRecords: map[string]string{"comment": "0123456789abcdef"}}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			panic(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.Body)); err != nil {
				panic(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	if err := gz.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// Server answers GitHub's download addresses (/<owner>/<repo>/tar.gz/HEAD)
// from archives, keyed by "<owner>/<repo>". Anything else is a 404, as it is
// on GitHub. It counts the requests it gets, so a test can say none came.
type Server struct {
	*httptest.Server
	Requests atomic.Int32

	mu    sync.Mutex
	paths []string
}

// Paths are the addresses asked for, in order.
func (s *Server) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// Serve starts a Server, closed when the test ends.
func Serve(t testing.TB, archives map[string][]byte) *Server {
	t.Helper()
	s := &Server{}
	paths := map[string][]byte{}
	for repo, body := range archives {
		paths["/"+repo+"/tar.gz/HEAD"] = body
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Requests.Add(1)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		body, ok := paths[r.URL.Path]
		if !ok {
			http.Error(w, "404: Not Found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}
