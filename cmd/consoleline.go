// package: cmd / cli
// type:    logic
// job:     single point of control for hoerbox's own stdout lines, plain and machine-readable
// limits:  no in-place progress line right now — see git history if that's wanted back
package cmd

import "fmt"

// printLine is hoerbox's one choke point for normal console output.
func printLine(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

// tokenPrefix marks a printToken line — see there.
const tokenPrefix = "@HOERBOX@"

// printToken emits a line a live GUI can recognize in hoerbox's own
// stdout, without a separate --json mode splitting "stream" into two
// outputs. kind is "status" (text: a line to show verbatim) or "refresh"
// (text: unused — a track finished, re-run "list --json").
func printToken(kind, text string) {
	fmt.Printf("%s %s %s\n", tokenPrefix, kind, text)
}
