package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

func newGetCommand(client *everybodycodes.Client) *cobra.Command {
	var downloadDir, year, questNumber string
	var force bool
	cmd := &cobra.Command{
		Use:     "get [<quest-url> | --quest <quest> [--year <year>]]",
		Short:   "Download and decrypt Everybody Codes quest inputs",
		Long:    "Download and decrypt Everybody Codes quest inputs using a quest URL or --quest with an optional --year (defaults to the current year). Quest numbers may have leading zeros.",
		Example: "  ec get https://everybody.codes/event/2024/quests/1\n  ec get -y 2024 -q 01\n  ec get --quest 1\n  ec get --year 2024 --quest 1 --dir solutions --force",
		Args: func(cmd *cobra.Command, args []string) error {
			yearSet, questSet := cmd.Flags().Changed("year"), cmd.Flags().Changed("quest")
			if len(args) != 0 {
				if yearSet || questSet {
					return fmt.Errorf("use either a quest URL or --year and --quest, not both")
				}
				return cobra.ExactArgs(1)(cmd, args)
			}
			if questSet {
				return nil
			}
			if yearSet {
				return fmt.Errorf("--quest is required when --year is supplied")
			}
			if cmd.Flags().NFlag() == 0 {
				return nil
			}
			return fmt.Errorf("--quest is required when no quest URL is supplied")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if downloadDir == "" {
				return fmt.Errorf("--dir requires a nonempty directory")
			}
			if len(args) == 0 && !cmd.Flags().Changed("quest") {
				return cmd.Help()
			}
			var q everybodycodes.Quest
			var err error
			if cmd.Flags().Changed("quest") {
				q, err = everybodycodes.ParseQuestFlags(year, questNumber)
			} else {
				q, err = everybodycodes.ParseQuestURL(args[0])
			}
			if err != nil {
				return err
			}
			return client.Download(cmd.Context(), q, downloadDir, force, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&downloadDir, "dir", ".", "Directory for quest inputs")
	cmd.Flags().StringVarP(&year, "year", "y", strconv.Itoa(time.Now().Year()), "Quest year (requires --quest; defaults to the current year)")
	cmd.Flags().StringVarP(&questNumber, "quest", "q", "", "Quest number in decimal (for example, 1 or 01)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing input files")
	return cmd
}
