package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// Run executes the CLI with the supplied arguments and version.
func Run(ctx context.Context, args []string, output io.Writer, version string) error {
	cmd := newRootCommand(version)
	if len(args) == 0 {
		args = []string{"--help"}
	}
	cmd.SetArgs(args)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(output)
	cmd.SetErr(output)
	return cmd.ExecuteContext(ctx)
}

func newRootCommand(version string) *cobra.Command {
	var downloadDir, statusDir, year, questNumber string
	var force bool
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
	get := &cobra.Command{
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
			var q quest
			var err error
			if cmd.Flags().Changed("quest") {
				q, err = parseQuestFlags(year, questNumber)
			} else {
				q, err = parseQuestURL(args[0])
			}
			if err != nil {
				return err
			}
			return initQuest(cmd.Context(), q, downloadDir, force, cmd.OutOrStdout())
		},
	}
	root.SetVersionTemplate("ec {{.Version}}\n")
	get.Flags().StringVar(&downloadDir, "dir", ".", "Directory for quest inputs")
	get.Flags().StringVarP(&year, "year", "y", strconv.Itoa(time.Now().Year()), "Quest year (requires --quest; defaults to the current year)")
	get.Flags().StringVarP(&questNumber, "quest", "q", "", "Quest number in decimal (for example, 1 or 01)")
	get.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing input files")
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Save a session cookie interactively",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuth(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	// A cookie supplied as a flag must not appear in the parser's error.
	auth.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		return fmt.Errorf("unsupported auth flags; run %s --help for usage", cmd.CommandPath())
	})
	auth.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Verify the saved session",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthCheck(cmd.Context(), cmd.OutOrStdout())
		},
	})
	status := &cobra.Command{
		Use:   "status",
		Short: "Show local input readiness by year, quest, and part",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if statusDir == "" {
				return fmt.Errorf("--dir requires a nonempty directory")
			}
			return runStatus(statusDir, cmd.OutOrStdout())
		},
	}
	status.Flags().StringVar(&statusDir, "dir", ".", "Directory containing local quest inputs")
	root.AddCommand(auth, get, status, newSubmitCommand(), newUpdateCommand(version))
	return root
}
