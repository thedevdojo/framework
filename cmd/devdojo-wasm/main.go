//go:build js && wasm

// Command devdojo-wasm is the WebAssembly surface over the engine: the
// same JSON compile protocol the CLI speaks over stdin/stdout, exposed to a
// JavaScript host as a single synchronous function. It exists so a Node
// program (the DevBlog CLI) can render a site with the exact engine DevDojo
// runs in production — no PHP, no native binary, no subprocess.
//
// The host loads the module with Go's wasm_exec.js, runs it, and then calls
//
//	globalThis.__devdojo.compile(inputJSON) → outputJSON
//	globalThis.__devdojo.version()          → "1.0.0"
//
// compile never throws for a bad INPUT: exactly like protocol.Run, malformed
// JSON or a render-time problem is folded into the output document's
// diagnostics so the host always gets back parseable JSON. main blocks
// forever after registering the functions — a wasm_exec program that returns
// tears down its exports.
package main

import (
	"bytes"
	"strings"
	"syscall/js"

	"github.com/thedevdojo/framework/blade"
	"github.com/thedevdojo/framework/protocol"
)

func compile(this js.Value, args []js.Value) any {
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return `{"engine":"` + blade.Version + `","files":{},"deps":{},"diagnostics":[{"file":"","line":0,"col":0,"kind":"error","message":"compile expects one JSON string argument"}]}`
	}
	var out bytes.Buffer
	if err := protocol.Run(strings.NewReader(args[0].String()), &out); err != nil {
		// Run only fails when writing the output fails; a bytes.Buffer never
		// does, but keep the contract honest rather than panicking the host.
		return `{"engine":"` + blade.Version + `","files":{},"deps":{},"diagnostics":[{"file":"","line":0,"col":0,"kind":"error","message":"` + err.Error() + `"}]}`
	}
	return out.String()
}

func version(this js.Value, args []js.Value) any {
	return blade.Version
}

func main() {
	api := js.Global().Get("Object").New()
	api.Set("compile", js.FuncOf(compile))
	api.Set("version", js.FuncOf(version))
	js.Global().Set("__devdojo", api)
	select {}
}
