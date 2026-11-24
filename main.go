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
	end(ctx, cli.Execute(ctx, version))
}

// end ends axon after a command that ran with ctx returned err. A command
// that failed after a signal ends by that signal, with one line; its own
// error is not printed. A command that finished its work before it acted
// on the signal has printed its result, and ends as it would have.
func end(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if sig, ok := interrupt.Signal(ctx); ok {
		interrupt.Report(os.Stderr)
		interrupt.Exit(sig)
	}
	fail.Exit(err)
}
