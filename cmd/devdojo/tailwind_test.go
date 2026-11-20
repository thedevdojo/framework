package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A stand-in for Tailwind's release download: one file, counted.
func tailwindRelease(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/"+tailwindVersion+"/tailwindcss-macos-arm64" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s, &hits
}

// The CLI is downloaded once, checked, kept, and runnable; the next build
// uses the kept one without asking again.
func TestFetchTailwindDownloadsOnceAndKeepsIt(t *testing.T) {
	body := "#!/bin/sh\necho tailwind\n"
	sum := sha256.Sum256([]byte(body))
	old := tailwindSums["macos-arm64"]
	tailwindSums["macos-arm64"] = hex.EncodeToString(sum[:])
	t.Cleanup(func() { tailwindSums["macos-arm64"] = old })

	server, hits := tailwindRelease(t, body)
	folder := filepath.Join(t.TempDir(), "devdojo")
	for i := 0; i < 2; i++ {
		got, err := fetchTailwind(folder, server.URL, "macos-arm64")
		if err != nil {
			t.Fatalf("fetchTailwind: %v", err)
		}
		if want := filepath.Join(folder, "tailwindcss-"+tailwindVersion+"-macos-arm64"); got != want {
			t.Errorf("path = %s, want %s", got, want)
		}
		info, err := os.Stat(got)
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Errorf("the kept CLI is not runnable: %v %v", info, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("downloaded %d times, want once", n)
	}
}

// A download that is not the file Tailwind released is thrown away.
func TestFetchTailwindRefusesADownloadThatDoesNotMatch(t *testing.T) {
	server, _ := tailwindRelease(t, "not tailwind at all")
	folder := filepath.Join(t.TempDir(), "devdojo")
	_, err := fetchTailwind(folder, server.URL, "macos-arm64")
	if _, unavailable := err.(tailwindUnavailable); !unavailable || !strings.Contains(err.Error(), "not the one Tailwind released") {
		t.Fatalf("err = %v, want a tailwindUnavailable about the checksum", err)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Errorf("kept something: %v", entries)
	}
}

func TestLinkStylesheetTakesThePlaceOfTheBrowserScript(t *testing.T) {
	page := "<head>\n<link href=\"fonts.css\">\n" +
		`<script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script>` +
		"\n<style type=\"text/tailwindcss\">@theme {}</style>\n<style data-sn=\"1\" type=\"text/tailwindcss\">.a{}</style>" +
		"\n<style>.keep{}</style>\n</head>"
	got := linkStylesheet(page, "/assets/site.abc.css")
	want := "<head>\n<link href=\"fonts.css\">\n<link rel=\"stylesheet\" href=\"/assets/site.abc.css\">\n<style>.keep{}</style>\n</head>"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if blocks := tailwindBlocks(page); len(blocks) != 2 || blocks[0] != "@theme {}" || blocks[1] != ".a{}" {
		t.Errorf("tailwindBlocks = %q", blocks)
	}
}
