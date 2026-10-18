package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
	"github.com/chandlerklein/everybody-codes-cli/internal/workspace"
)

func newGetCommand(client *everybodycodes.Client) *cobra.Command {
	var downloadDir, year, questNumber string
	var force bool
	cmd := &cobra.Command{
		Use:   "get [<quest-url> | --quest <quest> [--year <year>]]",
		Short: "Download and decrypt Everybody Codes quest inputs",
		Long: "Download and decrypt Everybody Codes quest inputs using a quest URL, flags, or the current workspace directory. " +
			"Find the nearest .ec.json in the current directory or its parents and infer year and quest from <year>/quest-NN/part-NN. " +
			"Explicit flags override inferred values; a missing year defaults to the current year. Quest numbers may have leading zeros. " +
			"Downloads use the workspace root, or the current directory if there is no marker. --dir selects an exact workspace destination. " +
			"After a successful download, create .ec.json with version 1 if missing, including when all inputs already exist.",
		Example: "  ec get https://everybody.codes/event/2024/quests/1\n  ec get -y 2024 -q 01\n  ec get --quest 1\n  ec get --year 2024 --quest 1 --dir solutions --force\n  cd 2024/quest-01/part-01 && ec get",
		Args: func(cmd *cobra.Command, args []string) error {
			yearSet, questSet := cmd.Flags().Changed("year"), cmd.Flags().Changed("quest")
			if len(args) != 0 {
				if yearSet || questSet {
					return fmt.Errorf("use either a quest URL or --year and --quest, not both")
				}
				return cobra.ExactArgs(1)(cmd, args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, inferred, err := resolveWorkspace(downloadDir, cmd.Flags().Changed("dir"))
			if err != nil {
				return err
			}
			resolvedYear, resolvedQuest := year, questNumber
			if !cmd.Flags().Changed("year") && inferred.Year != "" {
				resolvedYear = inferred.Year
			}
			if !cmd.Flags().Changed("quest") {
				resolvedQuest = inferred.Quest
			}
			if len(args) == 0 && resolvedQuest == "" && !cmd.Flags().Changed("quest") && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			var q everybodycodes.Quest
			if len(args) != 0 {
				q, err = everybodycodes.ParseQuestURL(args[0])
			} else {
				if resolvedQuest == "" && !cmd.Flags().Changed("quest") {
					if cmd.Flags().Changed("year") {
						return fmt.Errorf("--quest is required when --year is supplied and no workspace quest directory supplies it; use -q <quest>")
					}
					return fmt.Errorf("--quest is required when no quest URL is supplied and no workspace quest directory supplies it; use -q <quest> or run inside a workspace's <year>/quest-NN directory")
				}
				q, err = everybodycodes.ParseQuestFlags(resolvedYear, resolvedQuest)
			}
			if err != nil {
				return err
			}
			if err := client.Download(cmd.Context(), q, root, force, cmd.OutOrStdout()); err != nil {
				return err
			}
			return workspace.Create(root)
		},
	}
	cmd.Flags().StringVar(&downloadDir, "dir", ".", "Exact workspace destination (defaults to discovered workspace or current directory)")
	cmd.Flags().StringVarP(&year, "year", "y", strconv.Itoa(time.Now().Year()), "Quest year (overrides workspace context; otherwise defaults to the current year)")
	cmd.Flags().StringVarP(&questNumber, "quest", "q", "", "Quest number in decimal (for example, 1 or 01)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing input files")
	return cmd
}
