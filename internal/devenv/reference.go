package devenv

import (
	"bytes"
	"fmt"
	"strings"
)

// Reference returns the command reference of axon devenv as a Markdown
// page, produced from the same text as the commands' --help.
func Reference() []byte {
	var b bytes.Buffer
	b.WriteString(`# axon devenv command reference

<!-- Generated from internal/devenv/commands.go by "go generate ./...". Do not edit. -->

` + "`axon devenv`" + ` runs SkyPilot commands: dev environments, GPUs, jobs and volumes.
Each command runs the SkyPilot command of the same name with the same
arguments, against the API server that ` + "`axon login`" + ` connected. Flags that
this page does not list are passed to SkyPilot unchanged.

Any command not on this page is refused. ` + "`axon devenv <command> --help`" + `
prints a command's help.

Hints in SkyPilot's output name ` + "`sky`" + ` commands. Where ` + "`axon devenv`" + ` covers one,
run it as ` + "`axon devenv <command>`" + `.
`)
	for _, g := range groups {
		fmt.Fprintf(&b, "\n## %s\n", g)
		for _, c := range commands {
			if c.group != g {
				continue
			}
			name := "axon devenv " + strings.Join(c.words, " ")
			fmt.Fprintf(&b, "\n### `%s`\n\n%s.\n\n```\n%s %s\n```\n\n", name, c.summary, name, c.args)
			for _, p := range strings.Split(c.description, "\n\n") {
				b.WriteString(p + "\n\n")
			}
			b.WriteString("Flags:\n\n")
			for _, f := range c.flags {
				fmt.Fprintf(&b, "- `%s`: %s\n", f.names, f.text)
			}
			b.WriteString("\nExamples:\n\n```\n")
			for i, e := range c.examples {
				if i > 0 {
					b.WriteString("\n")
				}
				fmt.Fprintf(&b, "# %s\n%s\n", e.comment, e.line)
			}
			b.WriteString("```\n")
		}
	}
	return b.Bytes()
}
