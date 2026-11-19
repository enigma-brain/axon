// Package devenv is the axon devenv command group, the user's way to the
// SkyPilot API server. It owns the group's whole command tree: the covered
// commands, the passthrough of their arguments to sky, and their help.
package devenv

//go:generate go run ./gendoc ../../docs/reference/devenv.md

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// errNoCommand reports that axon devenv was run without a command.
var errNoCommand = errors.New(`name a command for axon devenv to run. Run "axon devenv --help" for the commands it covers`)

// Command returns the devenv command.
func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "devenv <command> [args]",
		Short: "Run SkyPilot commands: dev environments, GPUs, jobs and volumes",
		// Arguments belong to the forwarded command, not to axon. Only
		// --help or -h directly after a covered command is axon's, and the
		// matcher below handles it.
		DisableFlagParsing: true,
		// Errors are printed once, by the caller, in axon's own format.
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return dispatch(cmd, args)
		},
	}
}

// dispatch matches args against the covered commands and prints help,
// refuses the command, or forwards it to sky.
func dispatch(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		writeOverview(cmd.ErrOrStderr())
		return errNoCommand
	}
	if isHelp(args[0]) {
		writeOverview(cmd.OutOrStdout())
		return nil
	}
	c, rest, ok := match(args)
	if !ok {
		if group, ok := groupOf(args[0]); ok && len(args) > 1 && isHelp(args[1]) {
			writeGroup(cmd.OutOrStdout(), args[0], group)
			return nil
		}
		return refusal(args)
	}
	if len(rest) > 0 && isHelp(rest[0]) {
		writeHelp(cmd.OutOrStdout(), c)
		return nil
	}
	return forward(cmd.Context(), cmd.ErrOrStderr(), c, rest)
}

// match finds the covered command that args begin with, and returns it with
// the arguments that follow its words.
func match(args []string) (command, []string, bool) {
	for _, c := range commands {
		n := len(c.words)
		if len(args) >= n && slices.Equal(args[:n], c.words) {
			return c, args[n:], true
		}
	}
	return command{}, nil, false
}

// groupOf reports whether word is the first word of a two-word covered
// command, such as "jobs", and returns the commands that begin with it.
func groupOf(word string) ([]command, bool) {
	var cs []command
	for _, c := range commands {
		if len(c.words) > 1 && c.words[0] == word {
			cs = append(cs, c)
		}
	}
	return cs, len(cs) > 0
}

// refusal returns the error for arguments that name no covered command.
func refusal(args []string) error {
	group, isGroup := groupOf(args[0])
	if !isGroup {
		return fmt.Errorf(`axon devenv does not cover %q. Run "axon devenv --help" for the commands it covers`, args[0])
	}
	name := args[0]
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		name += " " + args[1]
	}
	names := make([]string, len(group))
	for i, c := range group {
		names[i] = `"` + strings.Join(c.words, " ") + `"`
	}
	return fmt.Errorf("axon devenv does not cover %q. The %s commands it covers are %s",
		name, args[0], strings.Join(names, ", "))
}

// isHelp reports whether arg asks for axon's help.
func isHelp(arg string) bool { return arg == "--help" || arg == "-h" }

// writeOverview writes the help of axon devenv itself: every covered
// command, by group.
func writeOverview(w io.Writer) {
	fmt.Fprint(w, `Run SkyPilot commands: dev environments, GPUs, jobs and volumes.

Each command runs the SkyPilot command of the same name with the same
arguments, against the API server that "axon login" connected.

Usage:
  axon devenv <command> [args]
  axon devenv <command> --help
`)
	width := nameWidth(commands)
	for _, g := range groups {
		fmt.Fprintf(w, "\n%s:\n", g)
		for _, c := range commands {
			if c.group == g {
				fmt.Fprintf(w, "  %-*s  %s\n", width, strings.Join(c.words, " "), c.summary)
			}
		}
	}
	fmt.Fprint(w, `
Hints in SkyPilot's output name "sky" commands. Where axon devenv covers
one, run it as "axon devenv <command>".
`)
}

// writeGroup writes the help of a group word such as "jobs": the covered
// commands that begin with it.
func writeGroup(w io.Writer, word string, group []command) {
	fmt.Fprintf(w, "Usage:\n  axon devenv %s <command> [args]\n  axon devenv %s <command> --help\n\n%s:\n",
		word, word, group[0].group)
	width := nameWidth(group)
	for _, c := range group {
		fmt.Fprintf(w, "  %-*s  %s\n", width, strings.Join(c.words, " "), c.summary)
	}
}

// writeHelp writes the help of one covered command.
func writeHelp(w io.Writer, c command) {
	fmt.Fprintf(w, "%s.\n\nUsage:\n  axon devenv %s %s\n\n", c.summary, strings.Join(c.words, " "), c.args)
	for _, p := range paragraphs(c.description) {
		fmt.Fprintln(w, p)
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Flags:")
	for _, f := range c.flags {
		fmt.Fprintf(w, "  %s\n", f.names)
		for _, line := range wrap(f.text, lineWidth-6) {
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
	fmt.Fprint(w, "\nOther flags are passed to SkyPilot unchanged.\n\nExamples:\n")
	for i, e := range c.examples {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "  # %s\n  %s\n", e.comment, e.line)
	}
}

// lineWidth is the width help text is wrapped to.
const lineWidth = 80

// paragraphs returns the paragraphs of text, each wrapped to lineWidth. A
// paragraph whose lines begin with "- " is a list, and each item is wrapped
// with a hanging indent.
func paragraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		if !strings.HasPrefix(p, "- ") {
			out = append(out, strings.Join(wrap(p, lineWidth), "\n"))
			continue
		}
		var items []string
		for _, item := range strings.Split(strings.TrimPrefix(p, "- "), "\n- ") {
			lines := wrap(item, lineWidth-2)
			items = append(items, "- "+strings.Join(lines, "\n  "))
		}
		out = append(out, strings.Join(items, "\n"))
	}
	return out
}

// wrap splits text into lines of at most width characters, breaking at
// spaces. Line breaks in text are treated as spaces.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// nameWidth returns the width of the longest command name in cs.
func nameWidth(cs []command) int {
	width := 0
	for _, c := range cs {
		width = max(width, len(strings.Join(c.words, " ")))
	}
	return width
}
