// Package cli builds axon's command tree.
package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/enigma-brain/axon/internal/login"
	"github.com/enigma-brain/axon/internal/sky"
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

	root.AddCommand(newLogin(), sky.Command())
	return root
}

// Execute runs the axon command with the process's arguments.
func Execute(ctx context.Context, version string) error {
	return NewRoot(version).ExecuteContext(ctx)
}

func newLogin() *cobra.Command {
	var config, pathway string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in and connect every command group",
		Long: `Log in and connect every command group.

The first login needs your organization's config file, which --config adds. With
more than one pathway in the stored files, login asks which one you are
on, once; --pathway chooses without asking, and is how you switch.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return login.Run(cmd.Context(), login.Options{
				Config:  config,
				Pathway: pathway,
				In:      cmd.InOrStdin(),
				Out:     cmd.OutOrStdout(),
				Err:     cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().StringVar(&config, "config", "", "add your organization's config file from an https address, a file path, or - for standard input")
	cmd.Flags().StringVar(&pathway, "pathway", "", "the identifier of the pathway to log in by, from any stored config file")
	return cmd
}
