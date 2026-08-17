// package: main / entrypoint
// type:    entrypoint
// job:     hoerbox CLI: a whitelisted YouTube Music speaker client for a Raspberry Pi
// limits:  no logic here — delegates straight to cmd.Execute
package main

import "github.com/flocko-motion/hoerbox/cmd"

func main() {
	cmd.Execute()
}
