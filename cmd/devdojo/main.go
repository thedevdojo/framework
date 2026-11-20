// Command devdojo is the CLI surface over the Go engine: `compile` speaks
// the JSON protocol (package protocol) over stdin/stdout for host embedding,
// `lint` gives a human a readable diagnostics report for a site directory on
// disk, `convert` rewrites a site from one syntax into the other, `migrate`
// moves an older site into src/, `new` writes a site from a template on
// GitHub (new.go),
// `serve` shows a site in the browser as it is edited (serve.go), `build`
// writes it as static files (build.go), and
// `version` prints the engine version. Argument handling is hand-rolled —
// a handful of subcommands do not earn a flag-parsing framework.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thedevdojo/framework/blade"
	"github.com/thedevdojo/framework/blade/convert"
	"github.com/thedevdojo/framework/blade/render"
	"github.com/thedevdojo/framework/migrate"
	"github.com/thedevdojo/framework/protocol"
	"github.com/thedevdojo/framework/site"
	"github.com/thedevdojo/framework/vfs"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches on the first argument and returns the process exit code, so
// main stays a one-line os.Exit call and every path here is testable without
// an actual process exit.
func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "compile":
		return cmdCompile()
	case "lint":
		return cmdLint(args[1:])
	case "convert":
		return cmdConvert(args[1:])
	case "migrate":
		return cmdMigrate(args[1:])
	case "new":
		return cmdNew(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "build":
		return cmdBuild(args[1:])
	case "version":
		fmt.Printf("devdojo %s\n", blade.Version)
		return 0
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: devdojo <command> [arguments]

Commands:
  compile        Compile a site from the JSON protocol on stdin, write the result to stdout
  lint <dir>     Compile every page under <dir> and print diagnostics
  convert <dir> --to html|blade [--dry-run]
                 Rewrite every view under <dir> in the other syntax, in place.
                 The site must render exactly as before or nothing is changed.
                 With no <dir>: the same over the JSON protocol on stdin/stdout
  migrate <dir> [--dry-run]
                 Move a site kept in resources/ (or with a views/ folder) into
                 src/, with pages in src/pages and components in src/components.
                 The site must render exactly as before or nothing is moved.
                 With no <dir>: the same over the JSON protocol on stdin/stdout
  new <folder> [--template <name>]
                 Download a template from GitHub and write it into <folder>: the
                 starter (github.com/devdojo-templates/starter) unless --template
                 names another (blog, owner/repo, or https://github.com/owner/repo).
                 A folder that already has something in it is left alone
  serve [dir] [--port N] [--no-reload]
                 Serve the site in [dir] (default: this folder) at
                 http://localhost:8000. Each page is compiled when it is asked
                 for, and the browser reloads when a file changes
  build [dir] [--out dist] [--url https://example.com]
                 Write the site in [dir] (default: this folder) as static files
                 into dist/: clean addresses, a folder for each entry of a
                 dynamic page, public/ as it is, a built stylesheet, robots.txt
                 and sitemap.xml. Nothing is written if a page does not compile
  version        Print the engine version

A file is read in the syntax its extension names:
  .blade.php     Blade            @if, {{ $post->title }}, <x-sections.hero />
  .html          HTML             <If>, {post.title}, <SectionsHero />`)
}

// cmdCompile speaks the protocol over stdin/stdout. Per the protocol
// contract, a problem with the INPUT (malformed JSON, a missing entry, a
// render-time error) never reaches here as an error — protocol.Run folds all
// of that into the output document's diagnostics so a host always gets back
// parseable JSON. This only returns non-zero when Run itself fails, which by
// that same contract means writing the output failed (e.g. a closed pipe).
func cmdCompile() int {
	if err := protocol.Run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "devdojo compile:", err)
		return 1
	}
	return 0
}

// cmdLint compiles every page under <dir> (vfs.Dir, the same discovery the
// protocol uses when entries are omitted — protocol.DiscoverPages: every
// .blade.php and .html file in the site's pages folder (src/pages/), each read
// in the syntax its extension names) and prints each diagnostic
// as `file:line:col [kind] message`, sorted by file then line then column.
//
// Lint merges two sources: render.Compile's error/app-tier diagnostics, and
// render.Analyze's WARNING-kind hints (undeclared component attributes).
// Analyze is a lint-only pass on purpose — its warnings never travel the
// compile protocol stream (see render.Analyze), so a warning here is
// advisory: warnings alone leave the exit code at 0; only a KindError sets 1.
func cmdLint(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: devdojo lint <dir>")
		return 2
	}
	dir := args[0]
	// A nonexistent or non-directory argument is a usage error, not a clean
	// tree: without this guard vfs.Dir(dir) would list nothing, Compile would
	// find no pages, and lint would silently exit 0 — hiding a typo'd path.
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo lint: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "devdojo lint: %q is not a directory\n", dir)
		return 2
	}
	fs := vfs.Dir(dir)
	entries := protocol.DiscoverPages(fs)
	// A dynamic page is compiled as serve and the platform compile it: one
	// entry of its collection bound under the collection's name, the whole
	// collection as `entries` (dynamicPagePreview, pages.go).
	layout := site.Detect(fs)
	variants := make([]render.Variant, len(entries))
	for i, entry := range entries {
		variants[i] = render.Variant{Entry: entry}
		if data, dynamic := dynamicPagePreview(fs, layout, entry); dynamic {
			variants[i].Data = data
		}
	}
	result := render.CompileVariants(fs, variants, render.Options{})

	diags := append([]render.Diagnostic(nil), result.Diagnostics...)
	// Warnings are lint-only (undeclared attributes) and come from a separate
	// analysis pass so they never touch Compile's protocol diagnostic stream.
	diags = append(diags, render.Analyze(fs, entries)...)
	sort.Slice(diags, func(i, j int) bool {
		a, b := diags[i], diags[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})

	hasError := false
	for _, d := range diags {
		msg := d.Message
		if d.Construct != "" {
			msg = fmt.Sprintf("%s: %s", d.Construct, msg)
		}
		fmt.Printf("%s:%d:%d [%s] %s\n", d.File, d.Line, d.Col, d.Kind, msg)
		if d.Kind == render.KindError {
			hasError = true
		}
	}
	// What the site's author should know that is not a problem (it still keeps
	// its source in resources/, say) goes to stderr: stdout stays empty for a
	// clean site, and the exit code is untouched.
	for _, n := range result.Notices {
		fmt.Fprintln(os.Stderr, "devdojo: "+n.Message)
	}
	if hasError {
		return 1
	}
	return 0
}

const migrateUsage = "usage: devdojo migrate <dir> [--dry-run]"

// cmdMigrate moves a site in one of the older folder shapes into src/.
//
// With a directory it works on disk, in place. migrate.Site decides whether
// anything happens at all: the site moves whole, rendering byte for byte as
// before, or not a single file is touched and the reasons are printed (exit
// 1). --dry-run prints every move and stops.
//
// With no arguments it speaks JSON on stdin/stdout (protocol.RunMigrate), for
// a host that holds the site in memory.
func cmdMigrate(args []string) int {
	if len(args) == 0 {
		if err := protocol.RunMigrate(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "devdojo migrate:", err)
			return 1
		}
		return 0
	}

	var dir string
	dryRun := false
	for _, a := range args {
		switch {
		case a == "--dry-run":
			dryRun = true
		case strings.HasPrefix(a, "-") || dir != "":
			fmt.Fprintln(os.Stderr, migrateUsage)
			return 2
		default:
			dir = a
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, migrateUsage)
		return 2
	}
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo migrate: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "devdojo migrate: %q is not a directory\n", dir)
		return 2
	}

	res := migrate.Site(vfs.Dir(dir), render.Options{})
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Printf("%s %s\n", p.File, p.Message)
		}
		fmt.Printf("\nNothing was moved: %s first.\n", plural(len(res.Problems), "thing needs", "things need")+" fixing")
		return 1
	}
	if len(res.Moves) == 0 {
		fmt.Println("Nothing to move: this site is already in src/, with pages in src/pages and components in src/components.")
		return 0
	}
	if dryRun {
		for _, m := range res.Moves {
			fmt.Printf("%s → %s\n", m.From, m.To)
		}
		fmt.Printf("\nWould move %s to src/. Nothing was changed.\n", plural(len(res.Moves), "file", "files"))
		return 0
	}

	on := func(rel string) string { return filepath.Join(dir, filepath.FromSlash(rel)) }

	// Put every file in its new place before taking any old one away. A file
	// whose text changes is written new and its original kept until the end;
	// every other file is renamed. If anything fails, what was done is undone
	// and the site is exactly as it was.
	var renamed, written []migrate.Move
	undo := func() {
		for _, m := range written {
			os.Remove(on(m.To))
		}
		for _, m := range renamed {
			os.Rename(on(m.To), on(m.From))
		}
	}
	for _, m := range res.Moves {
		if err := os.MkdirAll(filepath.Dir(on(m.To)), 0o755); err != nil {
			undo()
			fmt.Fprintf(os.Stderr, "devdojo migrate: %v\nNothing was moved.\n", err)
			return 1
		}
		if body, changed := res.Files[m.To]; changed {
			mode := os.FileMode(0o644)
			if st, err := os.Stat(on(m.From)); err == nil {
				mode = st.Mode().Perm()
			}
			err = os.WriteFile(on(m.To), []byte(body), mode)
			if err == nil {
				written = append(written, m)
			}
		} else {
			err = os.Rename(on(m.From), on(m.To))
			if err == nil {
				renamed = append(renamed, m)
			}
		}
		if err != nil {
			undo()
			fmt.Fprintf(os.Stderr, "devdojo migrate: %v\nNothing was moved.\n", err)
			return 1
		}
	}
	for _, m := range written {
		if err := os.Remove(on(m.From)); err != nil {
			fmt.Fprintf(os.Stderr, "devdojo migrate: moved, but could not remove %s: %v\n", m.From, err)
			return 1
		}
	}
	// Take away the folders the move emptied, deepest first. os.Remove refuses
	// a folder that still holds something, which is exactly the rule wanted.
	emptied := map[string]bool{}
	for _, m := range res.Moves {
		for d := filepath.Dir(on(m.From)); d != filepath.Clean(dir) && strings.HasPrefix(d, filepath.Clean(dir)); d = filepath.Dir(d) {
			emptied[d] = true
		}
	}
	dirs := make([]string, 0, len(emptied))
	for d := range emptied {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		os.Remove(d)
	}

	fmt.Printf("Moved %s to src/.\n", plural(len(res.Moves), "file", "files"))
	return 0
}

const convertUsage = "usage: devdojo convert <dir> --to html|blade [--dry-run]"

// cmdConvert rewrites a site's views from one syntax into the other.
//
// With a directory it works on disk, in place: every view in the other syntax
// is written out in the requested one beside the original, and only once all
// of them are written are the originals removed. convert.Site decides whether
// that happens at all — the site converts whole, rendering byte for byte as
// before, or not a single file is touched and the reasons are printed (exit
// 1). --dry-run prints what would be written and stops.
//
// With no arguments it speaks JSON on stdin/stdout (protocol.RunConvert), for
// a host that holds the site in memory, exactly as compile does.
func cmdConvert(args []string) int {
	if len(args) == 0 {
		if err := protocol.RunConvert(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "devdojo convert:", err)
			return 1
		}
		return 0
	}

	var dir, to string
	dryRun := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dry-run":
			dryRun = true
		case arg == "--to":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "devdojo convert: --to needs a syntax, html or blade\n"+convertUsage)
				return 2
			}
			i++
			to = args[i]
		case strings.HasPrefix(arg, "--to="):
			to = strings.TrimPrefix(arg, "--to=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "devdojo convert: unknown option %q\n%s\n", arg, convertUsage)
			return 2
		case dir == "":
			dir = arg
		default:
			fmt.Fprintf(os.Stderr, "devdojo convert: one site at a time (got %q and %q)\n%s\n", dir, arg, convertUsage)
			return 2
		}
	}
	// "html" is the HTML syntax's name; "studio", its older one, is still taken.
	syntax, known := convert.ParseSyntax(to)
	if dir == "" || !known {
		if to != "" && !known {
			fmt.Fprintf(os.Stderr, "devdojo convert: unknown syntax %q — use --to html or --to blade\n", to)
		}
		fmt.Fprintln(os.Stderr, convertUsage)
		return 2
	}
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo convert: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "devdojo convert: %q is not a directory\n", dir)
		return 2
	}

	name := syntax.Name()
	res := convert.Site(vfs.Dir(dir), syntax, render.Options{})

	if len(res.Problems) > 0 {
		problems := res.Problems
		sort.SliceStable(problems, func(i, j int) bool {
			a, b := problems[i], problems[j]
			if a.File != b.File {
				return a.File < b.File
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			return a.Col < b.Col
		})
		for _, p := range problems {
			if p.Line > 0 {
				fmt.Printf("%s:%d:%d %s\n", p.File, p.Line, p.Col, p.Message)
			} else {
				fmt.Printf("%s %s\n", p.File, p.Message)
			}
		}
		fmt.Printf("\nNothing was converted: %s before this site can be written in %s.\n", plural(len(problems), "thing needs", "things need")+" fixing", name)
		return 1
	}
	if len(res.Files) == 0 {
		fmt.Printf("Nothing to convert: no %s pages or components under %s.\n", other(syntax).Ext(), filepath.Join(dir, filepath.FromSlash(site.Detect(vfs.Dir(dir)).Root)))
		return 0
	}

	// Pair each new file with the one it replaces, for the report and so the
	// new file can take the old one's permissions.
	replaces := map[string]string{}
	for _, old := range res.Removed {
		replaces[strings.TrimSuffix(old, other(syntax).Ext())+syntax.Ext()] = old
	}
	written := make([]string, 0, len(res.Files))
	for p := range res.Files {
		written = append(written, p)
	}
	sort.Strings(written)

	if dryRun {
		for _, p := range written {
			fmt.Printf("%s → %s\n", replaces[p], p)
		}
		fmt.Printf("\nWould convert %s to %s. Nothing was changed.\n", plural(len(written), "file", "files"), name)
		return 0
	}

	// Write every new file before removing any old one. If a write fails, take
	// back the ones already written: the site is then exactly as it was.
	for i, p := range written {
		mode := os.FileMode(0o644)
		if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(replaces[p]))); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), []byte(res.Files[p]), mode); err != nil {
			for _, done := range written[:i] {
				os.Remove(filepath.Join(dir, filepath.FromSlash(done)))
			}
			fmt.Fprintf(os.Stderr, "devdojo convert: %v\nNothing was converted.\n", err)
			return 1
		}
	}
	for _, old := range res.Removed {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(old))); err != nil {
			// The converted file is in place; the stale original would shadow
			// or collide with it, so say exactly which one is left.
			fmt.Fprintf(os.Stderr, "devdojo convert: converted, but could not remove %s: %v\n", old, err)
			return 1
		}
	}
	fmt.Printf("Converted %s to %s.\n", plural(len(written), "file", "files"), name)
	return 0
}

// other is the syntax a conversion to s starts from.
func other(s convert.Syntax) convert.Syntax {
	if s == convert.Blade {
		return convert.Studio
	}
	return convert.Blade
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
