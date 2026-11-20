package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/protocol"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

const buildUsage = "usage: devdojo build [dir] [--out dist] [--url https://example.com]"

// cmdBuild writes a site as static files, ready to host anywhere:
//
//   - every page at a clean address (src/pages/pricing.html → pricing/index.html),
//     the home page as index.html and the 404 page as 404.html;
//   - a dynamic page ([posts.slug].html) once for each entry of its
//     collection, in a folder named for the entry;
//   - public/** copied as it is;
//   - one built stylesheet in place of the Tailwind browser script, when
//     Tailwind's CLI can be had (tailwind.go);
//   - robots.txt, and sitemap.xml when the site's address is known, unless
//     the site keeps its own in public/.
//
// Which page answers which address is decided by resolvePage, the function
// `serve` answers with, so a built site holds exactly the pages serve showed.
// The platform's Exporter (app/Services/Sites/Exporter.php) is the reference
// for the rest.
//
// Nothing is written unless every page compiles with nothing to say. The new
// build is put together in a folder of its own beside the output and only
// then moved into place, so a failed build leaves the last one as it was.
func cmdBuild(args []string) int {
	dir, out, siteURL := "", "", ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value *string
		switch {
		case arg == "--out" || arg == "--url":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "devdojo build: %s needs a value\n%s\n", arg, buildUsage)
				return 2
			}
			i++
			value = &out
			if arg == "--url" {
				value = &siteURL
			}
			*value = args[i]
		case strings.HasPrefix(arg, "--out="):
			out = strings.TrimPrefix(arg, "--out=")
		case strings.HasPrefix(arg, "--url="):
			siteURL = strings.TrimPrefix(arg, "--url=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "devdojo build: unknown option %q\n%s\n", arg, buildUsage)
			return 2
		case dir == "":
			dir = arg
		default:
			fmt.Fprintf(os.Stderr, "devdojo build: one site at a time (got %q and %q)\n%s\n", dir, arg, buildUsage)
			return 2
		}
	}
	if dir == "" {
		dir = "."
	}
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo build: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "devdojo build: %q is not a directory\n", dir)
		return 2
	}
	if siteURL != "" {
		if _, ok := siteAddress(siteURL); !ok {
			fmt.Fprintf(os.Stderr, "devdojo build: --url needs the site's whole address, like https://example.com (got %q)\n%s\n", siteURL, buildUsage)
			return 2
		}
	}
	files := vfs.Dir(dir)
	layout := site.Detect(files)
	if out == "" {
		out = filepath.Join(dir, "dist")
	}
	if msg := checkOut(dir, out, layout); msg != "" {
		fmt.Fprintf(os.Stderr, "devdojo build: %s\n%s\n", msg, buildUsage)
		return 2
	}
	if msg := outIsFree(dir, out); msg != "" {
		fmt.Fprintln(os.Stderr, "devdojo build: "+msg)
		return 1
	}

	if len(protocol.DiscoverPages(files)) == 0 {
		fmt.Fprintf(os.Stderr, "devdojo build: there are no pages in %s, so there is nothing to build. A site keeps its pages in src/pages/.\n", filepath.Join(dir, filepath.FromSlash(layout.Pages)))
		return 1
	}

	// The pages, and the address each is written at.
	plan := planBuild(files, layout)
	for _, note := range plan.notes {
		fmt.Fprintln(os.Stderr, "devdojo: "+note)
	}
	result := render.CompileVariants(files, plan.variants, render.Options{})
	said := map[string]bool{}
	for _, n := range result.Notices {
		if line := "devdojo: " + n.Message; !said[line] {
			said[line] = true
			fmt.Fprintln(os.Stderr, line)
		}
	}
	if len(result.Diagnostics) > 0 {
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
		seen := map[string]bool{}
		count := 0
		for _, d := range diags {
			// A layout's mistake is reported once for every page it frames.
			if line := diagnosticLine(d); !seen[line] {
				seen[line] = true
				count++
				fmt.Fprintln(os.Stderr, line)
			}
		}
		fmt.Fprintf(os.Stderr, "\nNothing was built: %s first.\n", plural(count, "thing needs", "things need")+" fixing")
		return 1
	}

	output := map[string][]byte{}
	pages := []string{}
	for _, v := range plan.variants {
		body, ok := result.Files[v.Output]
		if !ok {
			continue
		}
		output[v.Output] = []byte(body)
		pages = append(pages, v.Output)
	}
	sort.Strings(pages)

	// The stylesheet, built from what the pages ended up holding.
	stylesheet := ""
	if built, ok, err := buildStylesheet(dir, layout, output, pages); err != nil {
		var fallback tailwindUnavailable
		if !errors.As(err, &fallback) {
			fmt.Fprintf(os.Stderr, "devdojo build: the stylesheet did not build:\n%v\n\nNothing was built.\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "devdojo: %v, so the pages keep the Tailwind browser script instead of a built stylesheet. They look the same, but style themselves a moment after they load.\n", err)
	} else if ok {
		stylesheet = built
	}

	// public/** as it is. A file there is served at its own address before
	// any page, by serve and by a host, so it wins here too.
	publicCount := 0
	publicRoot := filepath.Join(dir, "public")
	err = filepath.WalkDir(publicRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == publicRoot && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || d.Name() == ".DS_Store" {
			return nil
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(publicRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if _, page := output[rel]; page {
			fmt.Fprintf(os.Stderr, "devdojo: public/%s is written at /%s, in place of the page that would be there\n", rel, rel)
		}
		output[rel] = body
		publicCount++
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo build: reading public/: %v\nNothing was built.\n", err)
		return 1
	}

	// robots.txt and sitemap.xml, unless the site keeps its own.
	base, known := "", false
	if siteURL != "" {
		base, known = siteAddress(siteURL)
	} else if raw, ok := files.Read(layout.SiteData()); ok {
		var data struct {
			URL any `json:"url"`
		}
		if json.Unmarshal(raw, &data) == nil {
			if s, isString := data.URL.(string); isString && s != "" {
				if base, known = siteAddress(s); !known {
					fmt.Fprintf(os.Stderr, "devdojo: the url in %s (%q) is not a whole address like https://example.com, so it is not used\n", layout.SiteData(), s)
				}
			}
		}
	}
	_, ownSitemap := output["sitemap.xml"]
	if _, own := output["robots.txt"]; !own {
		// Pointing at the sitemap needs the whole address too.
		robots := "User-agent: *\nAllow: /\n"
		if known {
			robots += "\nSitemap: " + base + "/sitemap.xml\n"
		}
		output["robots.txt"] = []byte(robots)
	}
	if !ownSitemap {
		if known {
			output["sitemap.xml"] = []byte(sitemap(base, pages))
		} else {
			fmt.Fprintf(os.Stderr, "devdojo: no sitemap.xml: it needs the site's address. Give it with --url https://example.com, or put \"url\" in %s\n", layout.SiteData())
		}
	}

	if err := writeBuild(out, output); err != nil {
		fmt.Fprintf(os.Stderr, "devdojo build: %v\nNothing was built.\n", err)
		return 1
	}

	summary := fmt.Sprintf("Built %s into %s: %s, %s from public/", dir, out, plural(len(pages), "page", "pages"), plural(publicCount, "file", "files"))
	if stylesheet != "" {
		summary += ", and the stylesheet " + stylesheet
	}
	fmt.Println(summary + ".")
	return 0
}

// buildPlan is what a build compiles: one variant for each page it writes,
// with the output path it is written at, and notes for the terminal about
// pages that are not written.
type buildPlan struct {
	variants []render.Variant
	notes    []string
}

// planBuild decides which pages a build writes, and where. Every page in the
// pages folder is asked about at the address it would be written at, and is
// written only when it is the page resolvePage answers there with: so a page
// in the HTML syntax with a Blade twin, or a folder's index page beside a
// flat page of the same name, is left out exactly as serve leaves it out.
func planBuild(files vfs.FS, layout site.Layout) buildPlan {
	var plan buildPlan
	written := map[string]bool{}
	add := func(v render.Variant) {
		if !written[v.Output] {
			written[v.Output] = true
			plan.variants = append(plan.variants, v)
		}
	}
	var dynamic []string
	for _, page := range protocol.DiscoverPages(files) {
		if _, _, ok := parseDynamicPage(page); ok {
			dynamic = append(dynamic, page)
			continue
		}
		output := exportPath(page)
		target, status, _ := resolvePage(files, "/"+addressOf(output), true)
		if status != 200 || target.entry != page {
			plan.notes = append(plan.notes, fmt.Sprintf("%s is not built: %s answers /%s", page, target.entry, addressOf(output)))
			continue
		}
		add(render.Variant{Entry: page, Output: output})
	}
	// Static pages first: an address a static page answers is never a
	// dynamic page's, here as in serve.
	for _, page := range dynamic {
		urls := dynamicPageURLs(files, layout, page)
		made := 0
		for _, u := range urls {
			target, status, _ := resolvePage(files, "/"+u, true)
			if status != 200 || target.entry != page {
				continue
			}
			add(render.Variant{Entry: page, Data: target.data, Output: u + "/index.html"})
			made++
		}
		if made == 0 {
			collection, field, _ := parseDynamicPage(page)
			plan.notes = append(plan.notes, fmt.Sprintf("%s makes no pages: no entry of %s has a %s that can be an address", page, collection, field))
		}
	}
	return plan
}

// exportPath is where a page is written: its output name (site.OutputName)
// with every page but the home page, a folder's index page and the 404 page
// moved into a folder of its own, so it answers at a clean address
// (pricing.html → pricing/index.html). The platform's
// BladePathMapper::exportPath.
func exportPath(page string) string {
	flat := site.OutputName(page)
	if flat == "404.html" || path.Base(flat) == "index.html" {
		return flat
	}
	return strings.TrimSuffix(flat, ".html") + "/index.html"
}

// addressOf is the address a written page answers at, without the leading
// slash: index.html → "", about/index.html → about, 404.html → 404.
func addressOf(output string) string {
	if output == "index.html" {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(output, "index.html"), ".html")
}

// siteAddress checks a site's address and returns it without a trailing
// slash: an http or https URL with a host and nothing after its path.
func siteAddress(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimSuffix(u.String(), "/"), true
}

// sitemap lists every written page but the 404 page at its address, the home
// page first and the rest in order (the platform's Exporter::sitemap). There
// is no lastmod: a file's time on disk says when it was copied, not when the
// page last changed.
func sitemap(base string, pages []string) string {
	var locs []string
	for _, p := range pages {
		if p == "404.html" {
			continue
		}
		locs = append(locs, base+"/"+strings.TrimSuffix(addressOf(p), "/"))
	}
	sort.Strings(locs)
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, loc := range locs {
		b.WriteString("  <url><loc>" + html.EscapeString(loc) + "</loc></url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

// checkOut refuses an output folder that would write over the site: the
// site's folder itself, a folder above it, or anything inside its source or
// public/.
func checkOut(dir, out string, layout site.Layout) string {
	absDir, err1 := filepath.Abs(dir)
	absOut, err2 := filepath.Abs(out)
	if err1 != nil || err2 != nil {
		return "cannot work out where the site and the output are"
	}
	if absOut == absDir || within(absDir, absOut) {
		return fmt.Sprintf("--out %s would write over the site itself; pick a folder beside it or inside it, like %s", out, filepath.Join(dir, "dist"))
	}
	for _, own := range []string{strings.TrimSuffix(layout.Root, "/"), "src", "resources", "public"} {
		if own == "" {
			continue
		}
		if folder := filepath.Join(absDir, own); absOut == folder || within(absOut, folder) {
			return fmt.Sprintf("--out %s is inside the site's %s/ folder, which build reads from; pick another, like %s", out, own, filepath.Join(dir, "dist"))
		}
	}
	return ""
}

// outIsFree refuses an output folder outside the site that already has
// something in it: build replaces a folder inside the site (its last build),
// and never empties one anywhere else.
func outIsFree(dir, out string) string {
	absDir, _ := filepath.Abs(dir)
	absOut, _ := filepath.Abs(out)
	if within(absOut, absDir) {
		return ""
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) == 0 {
		return ""
	}
	return fmt.Sprintf("%s is not empty, and build only empties a folder inside the site. Empty it, or pick another --out.", out)
}

// within reports whether path is inside folder (and not folder itself).
func within(path, folder string) bool {
	rel, err := filepath.Rel(folder, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeBuild writes the files into a new folder beside out, then puts that
// folder in out's place. The last build is only taken away once the new one
// is complete.
func writeBuild(out string, files map[string][]byte) error {
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	parent := filepath.Dir(absOut)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".devdojo-build-")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(tmp)
		}
	}()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		clean := path.Clean(name)
		if clean != name || path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(name, "\\\x00") {
			return fmt.Errorf("%s is not a name a file can be written at", name)
		}
		target := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, files[name], 0o644); err != nil {
			return err
		}
	}
	// os.MkdirTemp makes the folder readable by its owner only.
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}

	var old string
	if _, err := os.Stat(absOut); err == nil {
		old = tmp + "-old"
		if err := os.Rename(absOut, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, absOut); err != nil {
		if old != "" {
			os.Rename(old, absOut)
		}
		return err
	}
	done = true
	if old != "" {
		os.RemoveAll(old)
	}
	return nil
}
