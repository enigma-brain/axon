// Command axon is the command-line entry point to the platform.
package main

import (
	"context"

	"github.com/enigma-brain/axon/internal/cli"
	"github.com/enigma-brain/axon/internal/fail"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	if err := cli.Execute(context.Background(), version); err != nil {
		fail.Exit(err)
	}
}
