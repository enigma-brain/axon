// Command axon is the command-line entry point to the platform.
package main

import (
	"context"
	"os"

	"github.com/enigma-brain/axon/internal/cli"
	"github.com/enigma-brain/axon/internal/fail"
	"github.com/enigma-brain/axon/internal/interrupt"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	ctx, _ := interrupt.Notify(context.Background())
	err := cli.Execute(ctx, version)
	// A command that finished its work before it saw the signal has
	// printed its result, and ends as it would have.
	if sig, ok := interrupt.Signal(ctx); ok && err != nil {
		interrupt.Report(os.Stderr)
		interrupt.Exit(sig)
	}
	if err != nil {
		fail.Exit(err)
	}
}
