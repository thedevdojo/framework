// Command pocketknife is the CLI surface over the Go engine: `compile` speaks
// the JSON protocol (package protocol) over stdin/stdout for host embedding,
// `lint` gives a human a readable diagnostics report for a site directory on
// disk, and `version` prints the engine version. Argument handling is
// hand-rolled — three subcommands do not earn a flag-parsing framework.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/thedevdojo/pocketknife/blade"
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
  version        Print the engine version`)
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

// cmdLint compiles every page under <dir> (vfs.Dir, the same
// resources/views/pages/**.blade.html discovery the protocol uses when
// entries are omitted — protocol.DiscoverPages) and prints each diagnostic
// as `file:line:col [kind] message`, sorted by file then line then column.
// v1 lint is engine diagnostics only: it does not attempt undeclared-
// attribute detection, which is not implemented as a diagnostic anywhere in
// the render package.
func cmdLint(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pocketknife lint <dir>")
		return 2
	}
	dir := args[0]
	fs := vfs.Dir(dir)
	entries := protocol.DiscoverPages(fs)
	result := render.Compile(fs, entries, render.Options{})

	diags := append([]render.Diagnostic(nil), result.Diagnostics...)
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
