package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/thedevdojo/framework/starter"
)

const newUsage = "usage: devdojo new <folder>"

// cmdNew writes the starter site into a new folder: the src/ layout, in the
// HTML syntax, with public/ beside it. The site comes from inside the binary
// (package starter), so it needs no network and is always one this version
// reads.
//
// A folder that already has something in it is refused and left as it was
// (exit 1). There are no flags yet: anything that looks like one is a usage
// error (exit 2), so a later `--template` is not silently taken for a folder.
func cmdNew(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, newUsage)
		return 2
	}
	dir := args[0]

	count, err := starter.Write(dir)
	if errors.Is(err, starter.ErrNotEmpty) {
		fmt.Fprintf(os.Stderr, "devdojo new: %s is not empty. Pick a folder that does not exist yet, or an empty one.\n", dir)
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "devdojo new:", err)
		return 1
	}

	fmt.Printf("Created %s: %d files.\n\n  cd %s\n  devdojo serve\n", dir, count, dir)
	return 0
}
