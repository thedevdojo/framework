package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/thedevdojo/framework/starter"
)

const newUsage = "usage: devdojo new <folder> [--template <name>]"

// cmdNew downloads a template from GitHub and writes it into a new folder
// (package starter). With no --template it is the starter,
// github.com/devdojo-templates/starter. --template takes a bare name (a
// template of devdojo-templates), owner/repo, or the https address of a
// repository on GitHub.
//
// A folder that already has something in it is refused before anything is
// downloaded, and left as it was (exit 1). Anything that goes wrong after
// that leaves nothing behind.
func cmdNew(args []string) int {
	dir, name, asked := "", "", false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--template":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "devdojo new: --template needs a name\n"+newUsage)
				return 2
			}
			i++
			name, asked = args[i], true
		case strings.HasPrefix(arg, "--template="):
			name, asked = strings.TrimPrefix(arg, "--template="), true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "devdojo new: unknown option %q\n%s\n", arg, newUsage)
			return 2
		case dir == "":
			dir = arg
		default:
			fmt.Fprintf(os.Stderr, "devdojo new: one folder at a time (got %q and %q)\n%s\n", dir, arg, newUsage)
			return 2
		}
	}
	if dir == "" || (asked && strings.TrimSpace(name) == "") {
		fmt.Fprintln(os.Stderr, newUsage)
		return 2
	}
	tpl, err := starter.Parse(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo new: %v\n%s\n", err, newUsage)
		return 2
	}

	err = starter.CheckFolder(dir)
	if err == nil {
		fmt.Printf("Downloading %s…\n", tpl)
	}
	count := 0
	if err == nil {
		count, err = starter.Fetcher{}.Write(tpl, dir)
	}
	if errors.Is(err, starter.ErrNotEmpty) {
		fmt.Fprintf(os.Stderr, "devdojo new: %s is not empty. Pick a folder that does not exist yet, or an empty one.\n", dir)
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "devdojo new: %v.\n", err)
		return 1
	}

	fmt.Printf("Created %s from %s: %d files.\n\n  cd %s\n  devdojo serve\n", dir, tpl, count, dir)
	return 0
}
