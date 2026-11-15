// Command pocketknife is the CLI surface over the Go engine: `compile` speaks
// the JSON protocol (package protocol) over stdin/stdout for host embedding,
// `lint` gives a human a readable diagnostics report for a site directory on
// disk, `convert` rewrites a site from one syntax into the other, and
// `version` prints the engine version. Argument handling is hand-rolled —
// four subcommands do not earn a flag-parsing framework.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thedevdojo/pocketknife/blade"
	"github.com/thedevdojo/pocketknife/blade/convert"
	"github.com/thedevdojo/pocketknife/blade/render"
	"github.com/thedevdojo/pocketknife/protocol"
	"github.com/thedevdojo/pocketknife/vfs"
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
	case "version":
		fmt.Printf("pocketknife %s\n", blade.Version)
		return 0
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: pocketknife <command> [arguments]

Commands:
  compile        Compile a site from the JSON protocol on stdin, write the result to stdout
  lint <dir>     Compile every page under <dir> and print diagnostics
  convert <dir> --to html|blade [--dry-run]
                 Rewrite every view under <dir> in the other syntax, in place.
                 The site must render exactly as before or nothing is changed.
                 With no <dir>: the same over the JSON protocol on stdin/stdout
  version        Print the engine version

A file is read in the syntax its extension names:
  .blade.php     Blade   @if, {{ $post->title }}, <x-sections.hero />
  .html          HTML    <If>, {{ post.title }}, <SectionsHero />`)
}

// cmdCompile speaks the protocol over stdin/stdout. Per the protocol
// contract, a problem with the INPUT (malformed JSON, a missing entry, a
// render-time error) never reaches here as an error — protocol.Run folds all
// of that into the output document's diagnostics so a host always gets back
// parseable JSON. This only returns non-zero when Run itself fails, which by
// that same contract means writing the output failed (e.g. a closed pipe).
func cmdCompile() int {
	if err := protocol.Run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pocketknife compile:", err)
		return 1
	}
	return 0
}

// cmdLint compiles every page under <dir> (vfs.Dir, the same discovery the
// protocol uses when entries are omitted — protocol.DiscoverPages: every
// .blade.php and .html file under resources/views/pages/, each read in the
// syntax its extension names) and prints each diagnostic
// as `file:line:col [kind] message`, sorted by file then line then column.
//
// Lint merges two sources: render.Compile's error/app-tier diagnostics, and
// render.Analyze's WARNING-kind hints (undeclared component attributes).
// Analyze is a lint-only pass on purpose — its warnings never travel the
// compile protocol stream (see render.Analyze), so a warning here is
// advisory: warnings alone leave the exit code at 0; only a KindError sets 1.
func cmdLint(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pocketknife lint <dir>")
		return 2
	}
	dir := args[0]
	// A nonexistent or non-directory argument is a usage error, not a clean
	// tree: without this guard vfs.Dir(dir) would list nothing, Compile would
	// find no pages, and lint would silently exit 0 — hiding a typo'd path.
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pocketknife lint: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "pocketknife lint: %q is not a directory\n", dir)
		return 2
	}
	fs := vfs.Dir(dir)
	entries := protocol.DiscoverPages(fs)
	result := render.Compile(fs, entries, render.Options{})

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
	if hasError {
		return 1
	}
	return 0
}

const convertUsage = "usage: pocketknife convert <dir> --to html|blade [--dry-run]"

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
			fmt.Fprintln(os.Stderr, "pocketknife convert:", err)
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
				fmt.Fprintln(os.Stderr, "pocketknife convert: --to needs a syntax, html or blade\n"+convertUsage)
				return 2
			}
			i++
			to = args[i]
		case strings.HasPrefix(arg, "--to="):
			to = strings.TrimPrefix(arg, "--to=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "pocketknife convert: unknown option %q\n%s\n", arg, convertUsage)
			return 2
		case dir == "":
			dir = arg
		default:
			fmt.Fprintf(os.Stderr, "pocketknife convert: one site at a time (got %q and %q)\n%s\n", dir, arg, convertUsage)
			return 2
		}
	}
	syntax := convert.Syntax(strings.ToLower(to))
	if dir == "" || syntax.Ext() == "" {
		if to != "" && syntax.Ext() == "" {
			fmt.Fprintf(os.Stderr, "pocketknife convert: unknown syntax %q — use --to html or --to blade\n", to)
		}
		fmt.Fprintln(os.Stderr, convertUsage)
		return 2
	}
	info, err := os.Stat(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pocketknife convert: cannot access %q: %v\n", dir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "pocketknife convert: %q is not a directory\n", dir)
		return 2
	}

	name := map[convert.Syntax]string{convert.HTML: "the HTML syntax", convert.Blade: "Blade"}[syntax]
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
		fmt.Printf("Nothing to convert: no %s views under %s.\n", other(syntax).Ext(), filepath.Join(dir, "resources", "views"))
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
			fmt.Fprintf(os.Stderr, "pocketknife convert: %v\nNothing was converted.\n", err)
			return 1
		}
	}
	for _, old := range res.Removed {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(old))); err != nil {
			// The converted file is in place; the stale original would shadow
			// or collide with it, so say exactly which one is left.
			fmt.Fprintf(os.Stderr, "pocketknife convert: converted, but could not remove %s: %v\n", old, err)
			return 1
		}
	}
	fmt.Printf("Converted %s to %s.\n", plural(len(written), "file", "files"), name)
	return 0
}

// other is the syntax a conversion to s starts from.
func other(s convert.Syntax) convert.Syntax {
	if s == convert.Blade {
		return convert.HTML
	}
	return convert.Blade
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
