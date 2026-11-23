// Package sky is the axon sky command group, the user's way to the SkyPilot
// API server. It runs axon's own SkyPilot client with the user's arguments
// unchanged, against the API server that axon login connected, and refuses
// only the sky commands that would undo or bypass that connection.
package sky

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// Command returns the sky command.
func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "sky [command] [args]",
		Short: "Run SkyPilot's sky commands against the API server axon login connected",
		// Every argument, --help included, belongs to sky.
		DisableFlagParsing: true,
		// Errors are printed once, by the caller, in axon's own format.
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return dispatch(cmd.Context(), cmd.ErrOrStderr(), args)
		},
	}
}

// refusals are the sky api commands axon sky refuses, with the reason each
// gives. The endpoint, the login and the API server belong to axon login and
// axon logout, so these commands would undo or bypass them.
var refusals = map[string]string{
	"login":  "`axon sky api login` is not available. Run `axon login` to log in.",
	"logout": "`axon sky api logout` is not available. Run `axon logout` to log out.",
	"start":  "`axon sky api start` is not available: axon connects to your organization's API server and does not start one on this computer.",
	"stop":   "`axon sky api stop` is not available: axon connects to your organization's API server and does not start one on this computer.",
}

// dispatch refuses args, runs sky's own help, or forwards args to sky after
// the connection check. w receives what the check repairs.
func dispatch(ctx context.Context, w io.Writer, args []string) error {
	if err := refusal(args); err != nil {
		return err
	}
	dir, err := axondir.Default()
	if err != nil {
		return err
	}
	if wantsHelp(args) {
		return help(dir, args)
	}
	return forward(ctx, w, dir, args)
}

// refusal returns the error for a refused command, and nil for any other.
// A refused command is "api" followed by one of the refused words, as the
// first two words that are not flags; it is refused with --help too.
func refusal(args []string) error {
	words := commandWords(args, 2)
	if len(words) == 2 && words[0] == "api" {
		if msg, ok := refusals[words[1]]; ok {
			return errors.New(msg)
		}
	}
	return nil
}

// commandWords returns up to n of the leading arguments that are not flags,
// stopping at "--".
func commandWords(args []string, n int) []string {
	var words []string
	for _, a := range args {
		if a == "--" || len(words) == n {
			break
		}
		if len(a) > 0 && a[0] == '-' {
			continue
		}
		words = append(words, a)
	}
	return words
}

// wantsHelp reports whether sky would print help for args: when there are
// none, or when --help or -h comes before "--".
func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return true
	}
	end := len(args)
	if i := slices.Index(args, "--"); i >= 0 {
		end = i
	}
	return slices.Contains(args[:end], "--help") || slices.Contains(args[:end], "-h")
}

// help replaces the axon process with sky's own help for args. It needs the
// client and no login, so it runs no connection check; when a login is
// recorded, sky gets the same environment as a forwarded command.
func help(dir axondir.Dir, args []string) error {
	path, err := skyPath(dir)
	if errors.Is(err, skyclient.ErrNotInstalled) {
		return errNoClient
	}
	if err != nil {
		return fmt.Errorf("find the SkyPilot client: %w", err)
	}
	return run(path, args, helpEnviron(dir))
}
