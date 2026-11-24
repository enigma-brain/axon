// Package cli builds axon's command tree.
package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/enigma-brain/axon/internal/login"
	"github.com/enigma-brain/axon/internal/sky"
	"github.com/enigma-brain/axon/internal/update"
)

// NewRoot returns the axon command, which reports version for --version.
func NewRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:     "axon",
		Short:   "axon is the command-line entry point to the platform",
		Version: version,
		// Errors are printed once, by the caller, in axon's own format.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetVersionTemplate("axon {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{Use: "no-help", Hidden: true})

	root.AddCommand(newLogin(), newLogout(), sky.Command(), newUpdate(version))
	return root
}

// Execute runs the axon command with the process's arguments.
func Execute(ctx context.Context, version string) error {
	return NewRoot(version).ExecuteContext(ctx)
}

func newLogin() *cobra.Command {
	var opts login.Options
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in and connect every command group",
		Long: `Log in and connect every command group.

The first login makes a pathway: login asks how you reach the SkyPilot API
server, or takes the answers from --sky-endpoint, or from --teleport-proxy and
--sky-app. An organization with many members can hand out a config file
instead, which --config adds. With more than one stored pathway, login asks
which one you are on, once; --pathway chooses without asking, and is how you
switch.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.In, opts.Out, opts.Err = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return login.Run(cmd.Context(), opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Config, "config", "", "add your organization's config file from an https address, a file path, or - for standard input")
	f.StringVar(&opts.Pathway, "pathway", "", "the identifier of the pathway to log in by, from any stored config file")
	f.StringVar(&opts.SkyEndpoint, "sky-endpoint", "", "make a pathway to a SkyPilot API server you reach directly, at this https address")
	f.StringVar(&opts.TeleportProxy, "teleport-proxy", "", "make a pathway through Teleport, with this Teleport proxy (host or host:port)")
	f.StringVar(&opts.SkyApp, "sky-app", "", "with --teleport-proxy, the Teleport application that serves SkyPilot (default skypilot)")
	f.StringVar(&opts.Name, "name", "", "with --sky-endpoint or --teleport-proxy, the pathway's name (default: the address)")
	f.BoolVar(&opts.Replace, "replace", false, "answer yes to every question about replacing a stored pathway or config file")
	return cmd
}

func newLogout() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out and disconnect every command group",
		Long: `Log out and disconnect every command group.

Logs the SkyPilot client out, stops the Teleport tunnel, ends the Teleport
session of a Teleport pathway, and removes axon's records of the connection.
The config files and the active pathway are kept, so "axon login" logs in
again.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return login.Logout(cmd.Context(), login.LogoutOptions{
				Out: cmd.OutOrStdout(),
				Err: cmd.ErrOrStderr(),
			})
		},
	}
}

func newUpdate(current string) *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update axon to the latest release",
		Long: `Update axon to the latest release.

Downloads the latest release of axon for this machine, checks it against the
release's checksums, and replaces the axon you ran with it in one step. When
axon is already the latest release, nothing changes. --version installs a
named release instead, such as a pre-release or an older release.

Only the axon binary changes: your config files, login and the clients axon
installed are kept. axon looks for a new release only when you run this
command.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return update.Run(cmd.Context(), update.Options{
				Current: current,
				Version: version,
				Out:     cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "install this release instead of the latest, such as 1.2.3 or 1.2.3-rc.1")
	return cmd
}
