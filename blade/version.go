// Package blade is the root of the Go engine: the Blade-subset scanner,
// parser, expression evaluator, and renderer live in its subpackages
// (scanner, parser, expr, render). This file only carries the engine's
// version string, which the protocol package (protocol.Output.Engine) and
// the CLI (`pocketknife version`) both surface to callers — a host embedding
// Pocketknife can pin or detect engine capability against it.
package blade

// Version is Pocketknife's engine semver. Bump it when the rendered-output
// contract (grammar, diagnostics shape, or the protocol envelope) changes in
// a way a host might need to react to.
const Version = "0.1.0"
