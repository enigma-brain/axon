// Package fail prints axon's failure messages.
//
// The error that ends a command goes through this package, so that it goes
// to standard error, begins with "axon:", and is followed by a non-zero
// exit. A warning printed while a command carries on, such as a config file
// that could not be updated, is written by the command itself.
package fail

import (
	"fmt"
	"io"
	"os"
)

// Code is the exit code axon uses when it stops on its own failure.
const Code = 1

// Print writes err to w as one line beginning with "axon:".
func Print(w io.Writer, err error) {
	fmt.Fprintf(w, "axon: %v\n", err)
}

// Exit prints err to standard error and exits with Code.
func Exit(err error) {
	Print(os.Stderr, err)
	os.Exit(Code)
}
