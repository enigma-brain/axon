// Command gendoc writes the axon devenv command reference to the path given
// as its one argument. "go generate ./..." runs it.
package main

import (
	"fmt"
	"os"

	"github.com/enigma-brain/axon/internal/devenv"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gendoc <output path>")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], devenv.Reference(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
