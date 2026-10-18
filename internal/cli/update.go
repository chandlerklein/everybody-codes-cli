package cli

import (
	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/update"
)

func newUpdateCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update ec to the latest stable release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return update.Run(cmd.Context(), version, cmd.OutOrStdout())
		},
	}
}
