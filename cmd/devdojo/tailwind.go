package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/thedevdojo/framework/site"
)

// The stylesheet of a built site. The engine prints a site's stylesheet as
// the Tailwind browser script plus the raw CSS in <style type="text/tailwindcss">
// blocks, which is right while a site is being written: a class just typed
// styles itself. A built site gets one stylesheet compiled ahead of time by
// Tailwind's own standalone CLI instead, the way the platform's TailwindBuilder
// and StylesheetBlock do it.

// tailwindVersion is the Tailwind CLI a build uses: the one the platform runs.
const tailwindVersion = "v4.3.3"

// tailwindSums are the SHA-256 of each standalone CLI of tailwindVersion, from
// the release's sha256sums.txt. A download that does not match is thrown away.
var tailwindSums = map[string]string{
	"linux-arm64":      "55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195",
	"linux-arm64-musl": "71ea4be79c9de9827545682df3e040053fb535d37c71ed2cfdedf9385a0868e0",
	"linux-x64":        "dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a",
	"linux-x64-musl":   "a04d34ceacc8f52cbe8920ad846cdeb61d3d0021dba32db0d1f77c9d9fad7a6c",
	"macos-arm64":      "cdf646702987a743464dff4d9c60fd4480d1c1e73dd819a9a67f1078815dce9d",
	"macos-x64":        "7922e0953f2110c05976e3bf58f14e643d90427575e766b7d433f5f80cbee7e1",
	"windows-x64.exe":  "e0e260ce048014e9268f6237ff18f8ccf02cef521cbd0ae04e82c2cdf7aa3955",
}

// tailwindBinaryEnv names a Tailwind CLI to use instead of the downloaded one
// (the tests give a stand-in). tailwindURLEnv replaces where it is downloaded
// from.
const (
	tailwindBinaryEnv = "DEVDOJO_TAILWIND"
	tailwindURLEnv    = "DEVDOJO_TAILWIND_URL"
	tailwindURL       = "https://github.com/tailwindlabs/tailwindcss/releases/download"
)

// tailwindUnavailable is why a build has no Tailwind CLI to run. The build goes
// on with the browser script; any other stylesheet error stops it.
type tailwindUnavailable struct{ why string }

func (e tailwindUnavailable) Error() string { return e.why }

var (
	// tailwindScript is the browser script the engine prints (blade/render/vite.go),
	// matched loosely so a pinned version still matches.
	tailwindScript = regexp.MustCompile(`(?i)<script\s[^>]*src="https://cdn\.jsdelivr\.net/npm/@tailwindcss/browser@[^"]*"[^>]*>\s*</script>`)
	// styleBlock is any <style> element; tailwindStyle says whether it is a
	// text/tailwindcss one (Go's regexp cannot look ahead).
	styleBlock    = regexp.MustCompile(`(?is)\n?<style\b([^>]*)>(.*?)</style>`)
	tailwindStyle = regexp.MustCompile(`(?i)\stype="text/tailwindcss"`)
)

// scannable are the files Tailwind is told to look in for class names, beside
// the built pages: a class in a branch no page took, or one only a script
// adds, is still wanted (the platform's SiteStylesheet::SCANNABLE).
var scannable = []string{".blade.php", ".html", ".js", ".json", ".md"}

// buildStylesheet builds the site's stylesheet from the built pages and puts
// a link to it in their place of the browser script. It returns the
// stylesheet's path, or ok false when no page asked for a stylesheet. A
// tailwindUnavailable error leaves every page as it was.
func buildStylesheet(dir string, layout site.Layout, output map[string][]byte, pages []string) (name string, ok bool, err error) {
	// The sources, read back out of the pages: the engine has already
	// resolved what each layout's <Vite> named. First seen first, once each.
	var sources []string
	seen := map[string]bool{}
	for _, p := range pages {
		for _, css := range tailwindBlocks(string(output[p])) {
			if !seen[css] {
				seen[css] = true
				sources = append(sources, css)
			}
		}
	}
	if len(sources) == 0 {
		return "", false, nil
	}

	binary, err := findTailwind()
	if err != nil {
		return "", false, err
	}

	work, err := os.MkdirTemp("", "devdojo-css-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(work)
	if err := os.Mkdir(filepath.Join(work, "src"), 0o700); err != nil {
		return "", false, err
	}
	// Flattened under hashed names: Tailwind only reads the extension, and a
	// flat folder has no path to get wrong.
	put := func(name string, body []byte) error {
		sum := sha1.Sum([]byte(name))
		ext := ".html"
		for _, e := range scannable {
			if strings.HasSuffix(name, e) {
				ext = e
			}
		}
		if ext == ".blade.php" {
			ext = ".php"
		}
		return os.WriteFile(filepath.Join(work, "src", hex.EncodeToString(sum[:])+ext), body, 0o600)
	}
	for _, p := range pages {
		if err := put("rendered/"+p, output[p]); err != nil {
			return "", false, err
		}
	}
	for _, folder := range []string{strings.TrimSuffix(layout.Root, "/"), "public"} {
		if folder == "" {
			continue
		}
		root := filepath.Join(dir, folder)
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			for _, e := range scannable {
				if strings.HasSuffix(p, e) {
					if body, err := os.ReadFile(p); err == nil {
						rel, _ := filepath.Rel(dir, p)
						put(filepath.ToSlash(rel), body)
					}
					break
				}
			}
			return nil
		})
	}

	// As the browser script does it: a stylesheet with no @import gets
	// Tailwind put in front of it (quirk and all: any @import stops that).
	entry := strings.Join(sources, "\n")
	if !strings.Contains(entry, "@import") {
		entry = "@import \"tailwindcss\";\n" + entry
	}
	entry += "\n\n@source \"./src/**/*\";\n"
	if err := os.WriteFile(filepath.Join(work, "entry.css"), []byte(entry), 0o600); err != nil {
		return "", false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--input", filepath.Join(work, "entry.css"), "--output", filepath.Join(work, "out.css"), "--minify")
	cmd.Dir = work
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", false, fmt.Errorf("%s", msg)
		}
		return "", false, fmt.Errorf("Tailwind stopped: %v", err)
	}
	css, err := os.ReadFile(filepath.Join(work, "out.css"))
	if err != nil {
		return "", false, fmt.Errorf("Tailwind wrote no stylesheet")
	}

	sum := sha1.Sum(css)
	name = "assets/site." + hex.EncodeToString(sum[:])[:12] + ".css"
	if _, taken := output[name]; taken {
		return "", false, fmt.Errorf("%s is already a page or a file of the site", name)
	}
	for _, p := range pages {
		output[p] = []byte(linkStylesheet(string(output[p]), "/"+name))
	}
	output[name] = css
	return name, true, nil
}

// tailwindBlocks is the CSS of every text/tailwindcss block in a page, in order.
func tailwindBlocks(page string) []string {
	var out []string
	for _, m := range styleBlock.FindAllStringSubmatch(page, -1) {
		if tailwindStyle.MatchString(m[1]) {
			out = append(out, m[2])
		}
	}
	return out
}

// linkStylesheet puts a link to the built stylesheet where the browser script
// was, so it keeps its place in the head, and takes every text/tailwindcss
// block out with the newline before it. A page with neither is unchanged.
func linkStylesheet(page, href string) string {
	if loc := tailwindScript.FindStringIndex(page); loc != nil {
		page = page[:loc[0]] + `<link rel="stylesheet" href="` + href + `">` + page[loc[1]:]
	}
	return styleBlock.ReplaceAllStringFunc(page, func(block string) string {
		m := styleBlock.FindStringSubmatch(block)
		if tailwindStyle.MatchString(m[1]) {
			return ""
		}
		return block
	})
}

// findTailwind is the Tailwind CLI to run: the one $DEVDOJO_TAILWIND names,
// or the one kept in this machine's cache folder, downloaded the first time.
func findTailwind() (string, error) {
	if named, set := os.LookupEnv(tailwindBinaryEnv); set && named != "" {
		if info, err := os.Stat(named); err != nil || info.IsDir() {
			return "", tailwindUnavailable{fmt.Sprintf("%s names %s, which is not there", tailwindBinaryEnv, named)}
		}
		return named, nil
	}
	asset, ok := tailwindAsset()
	if !ok {
		return "", tailwindUnavailable{fmt.Sprintf("Tailwind has no standalone CLI for %s/%s", runtime.GOOS, runtime.GOARCH)}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", tailwindUnavailable{"there is no cache folder to keep Tailwind in"}
	}
	base := tailwindURL
	if env := os.Getenv(tailwindURLEnv); env != "" {
		base = strings.TrimSuffix(env, "/")
	}
	return fetchTailwind(filepath.Join(cache, "devdojo"), base, asset)
}

// tailwindAsset is the name of the Tailwind CLI built for this machine.
func tailwindAsset() (string, bool) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		return "", false
	}
	var asset string
	switch runtime.GOOS {
	case "darwin":
		asset = "macos-" + arch
	case "linux":
		asset = "linux-" + arch
		// Alpine and the like have musl, not glibc, and need their own build.
		if matches, _ := filepath.Glob("/lib/ld-musl-*.so.1"); len(matches) > 0 {
			asset += "-musl"
		}
	case "windows":
		asset = "windows-" + arch + ".exe"
	default:
		return "", false
	}
	_, ok := tailwindSums[asset]
	return asset, ok
}

// fetchTailwind returns the cached CLI in folder, downloading it from base
// when it is not there yet. The download is checked against tailwindSums and
// only kept when it matches.
func fetchTailwind(folder, base, asset string) (string, error) {
	name := "tailwindcss-" + tailwindVersion + "-" + strings.TrimSuffix(asset, ".exe")
	if strings.HasSuffix(asset, ".exe") {
		name += ".exe"
	}
	cached := filepath.Join(folder, name)
	if info, err := os.Stat(cached); err == nil && info.Mode().IsRegular() {
		return cached, nil
	}

	fmt.Fprintf(os.Stderr, "devdojo: downloading Tailwind CSS %s, once, to build stylesheets with…\n", tailwindVersion)
	unavailable := func(why string) error {
		return tailwindUnavailable{"couldn't download Tailwind CSS " + tailwindVersion + " (" + why + ")"}
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(base + "/" + tailwindVersion + "/tailwindcss-" + asset)
	if err != nil {
		return "", unavailable("no connection to GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", unavailable("GitHub answered " + resp.Status)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", unavailable(err.Error())
	}
	tmp, err := os.CreateTemp(folder, ".tailwindcss-")
	if err != nil {
		return "", unavailable(err.Error())
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	// A standalone CLI is about 100 MB; a download far past that is not one.
	_, err = io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, 400<<20))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", unavailable(err.Error())
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != tailwindSums[asset] {
		return "", unavailable("the file that came back is not the one Tailwind released")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", unavailable(err.Error())
	}
	if err := os.Rename(tmp.Name(), cached); err != nil {
		return "", unavailable(err.Error())
	}
	return cached, nil
}
