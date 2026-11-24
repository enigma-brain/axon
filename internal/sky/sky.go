// Package sky is everything in axon that is specific to SkyPilot. It is the
// axon sky command group, the user's way to the SkyPilot API server: it runs
// axon's own SkyPilot client with the user's arguments unchanged, against
// the API server that axon login connected, and refuses only the sky
// commands that would undo or bypass that connection. It also holds the
// steps axon login and axon logout run with SkyPilot, through Connector.
package sky

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// Run runs axon sky with args for the user whose axon directory is dir. w
// receives what the connection check repairs.
func Run(ctx context.Context, dir axondir.Dir, w io.Writer, args []string) error {
	return dispatch(ctx, dir, w, args)
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
func dispatch(ctx context.Context, dir axondir.Dir, w io.Writer, args []string) error {
	if err := refusal(args); err != nil {
		return err
	}
	if wantsHelp(args) {
		return help(dir, args)
	}
	err := forward(ctx, w, dir, args)
	if errors.Is(err, ErrNoLogin) && hasHelpFlag(args) {
		return fmt.Errorf("%w %s", err, msgHelpHint)
	}
	return err
}

// msgHelpHint follows the no-login message when a help flag was not taken
// for a request for help.
const msgHelpHint = "For a command's help, run `axon sky <command> --help`."

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

// commandWords returns up to n of the leading arguments that are not flags.
// A "--" is skipped, not taken as the end of the command's words: before a
// subcommand's name, sky's command groups take "--" for the end of their
// own options and still read the name after it, so "api -- logout" names
// api logout.
func commandWords(args []string, n int) []string {
	var words []string
	for _, a := range args {
		if len(words) == n {
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
// none, or when --help or -h comes before the "--" that ends a command's
// options and cannot be an option's value. axon does not know which of
// sky's options take a value, so a help flag counts only as the first
// argument, or after an argument that is not an option or that holds its
// own value, as --name=value does. After a bare option, as in "-c -h", sky
// can read the flag as the option's value and run the command, so it
// goes through the connection check instead. A "--" before the first word
// is the root group's, and sky reads the options after it.
func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return true
	}
	seenWord := false
	prev := ""
	for i, a := range args {
		switch {
		case a == "--" && seenWord:
			return false
		case isHelpFlag(a):
			if i == 0 || !isBareOption(prev) {
				return true
			}
		case !isOption(a):
			seenWord = true
		}
		prev = a
	}
	return false
}

// hasHelpFlag reports whether --help or -h comes before the "--" that ends
// a command's options, whether or not wantsHelp takes it for help.
func hasHelpFlag(args []string) bool {
	seenWord := false
	for _, a := range args {
		switch {
		case a == "--" && seenWord:
			return false
		case isHelpFlag(a):
			return true
		case !isOption(a):
			seenWord = true
		}
	}
	return false
}

func isHelpFlag(a string) bool { return a == "--help" || a == "-h" }

func isOption(a string) bool { return len(a) > 0 && a[0] == '-' }

// isBareOption reports whether a is an option that does not hold its own
// value, and so can be waiting for the next argument as its value. The
// "--" that ends the root group's options is not one.
func isBareOption(a string) bool {
	return isOption(a) && a != "--" && !strings.Contains(a, "=")
}

// help replaces the axon process with sky's own help for args. It needs the
// client and no login, so it runs no connection check, and runs sky against
// an endpoint where nothing answers.
func help(dir axondir.Dir, args []string) error {
	path, err := skyPath(dir)
	if errors.Is(err, skyclient.ErrNotInstalled) {
		return errNoClient
	}
	if err != nil {
		return fmt.Errorf("find the SkyPilot client: %w", err)
	}
	return run(path, args, helpEnviron(os.Environ()))
}
