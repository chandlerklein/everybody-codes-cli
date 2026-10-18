package cli

import (
	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

func newStatusCommand() *cobra.Command {
	var statusDir string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local input readiness by year, quest, and part",
		Long: "Show local input readiness from the nearest workspace marked by .ec.json in the current directory or its parents. " +
			"Without a marker, use the current directory. --dir selects an exact directory. This command never creates .ec.json.",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _, err := resolveWorkspace(statusDir, cmd.Flags().Changed("dir"))
			if err != nil {
				return err
			}
			return everybodycodes.Status(root, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&statusDir, "dir", ".", "Exact directory containing inputs (defaults to discovered workspace or current directory)")
	return cmd
}
