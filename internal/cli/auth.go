package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
	"github.com/chandlerklein/everybody-codes-cli/internal/session"
)

func newAuthCommand(client *everybodycodes.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Save a session cookie interactively",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuth(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	// A cookie supplied as a flag must not appear in the parser's error.
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		return fmt.Errorf("unsupported auth flags; run %s --help for usage", cmd.CommandPath())
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Verify the saved session",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthCheck(cmd.Context(), client, cmd.OutOrStdout())
		},
	})
	return cmd
}

func runAuthCheck(ctx context.Context, client *everybodycodes.Client, output io.Writer) error {
	cookie, err := session.Load()
	if err != nil {
		return fmt.Errorf("%w; run ec auth to save a session cookie", err)
	}
	if err := client.CheckSession(ctx, cookie); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "Session is valid.")
	return err
}
