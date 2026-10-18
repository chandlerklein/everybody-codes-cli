package cli

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

// Run executes the CLI with the supplied arguments and version.
func Run(ctx context.Context, args []string, output io.Writer, version string) error {
	return execute(ctx, args, os.Stdin, output, version, everybodycodes.NewClient(nil))
}

func execute(ctx context.Context, args []string, input io.Reader, output io.Writer, version string, client *everybodycodes.Client) error {
	cmd := newRootCommand(version, client)
	if len(args) == 0 {
		args = []string{"--help"}
	}
	cmd.SetArgs(args)
	cmd.SetIn(input)
	cmd.SetOut(output)
	cmd.SetErr(os.Stderr)
	return cmd.ExecuteContext(ctx)
}

func newRootCommand(version string, client *everybodycodes.Client) *cobra.Command {
	root := &cobra.Command{
		Use:           "ec",
		Version:       version,
		Short:         "Manage Everybody Codes quest inputs and submit answers",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("ec {{.Version}}\n")
	root.AddCommand(newAuthCommand(client), newGetCommand(client), newStatusCommand(), newSubmitCommand(client), newUpdateCommand(version))
	return root
}
