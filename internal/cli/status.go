package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

func newStatusCommand() *cobra.Command {
	var statusDir string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local input readiness by year, quest, and part",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if statusDir == "" {
				return fmt.Errorf("--dir requires a nonempty directory")
			}
			return everybodycodes.Status(statusDir, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&statusDir, "dir", ".", "Directory containing local quest inputs")
	return cmd
}
