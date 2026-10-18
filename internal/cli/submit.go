package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

func newSubmitCommand(client *everybodycodes.Client) *cobra.Command {
	var year, questNumber, partNumber string
	cmd := &cobra.Command{
		Use:   "submit [<ANSWER>]",
		Short: "Submit an Everybody Codes answer",
		Long: "Submit a UTF-8 answer as an argument or, when omitted, read it from standard input. " +
			"One final LF or CRLF is removed from standard input; other whitespace is preserved. " +
			"Quest numbers may have leading zeros. Part numbers must be positive decimal integers without leading zeros. " +
			"Find the nearest .ec.json in the current directory or its parents, then infer year, quest, and part from <year>/quest-NN/part-NN, including solver subdirectories. " +
			"Directory names must match the generated layout; only part-01, part-02, and part-03 supply a part. " +
			"Explicit flags override inferred values. Missing quest and part require --quest and --part; a missing year defaults to the current year. " +
			"Without .ec.json, use explicit quest and part flags; directory names are not inferred. " +
			"Correct answers automatically download newly unlocked inputs into the workspace root, or the current directory when no marker exists. This command never creates .ec.json.",
		Example: "  ec submit -y 2024 -q 1 -p 1 1309\n  ec submit -q 1 -p 1 'text answer'\n  ./solve | ec submit -y 2024 -q 1 -p 1\n  cd 2024/quest-01/part-01 && ec submit 1309\n  ./solve | ec submit",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, inferred, err := resolveWorkspace("", false)
			if err != nil {
				return err
			}
			resolvedYear, resolvedQuest, resolvedPart := year, questNumber, partNumber
			if !cmd.Flags().Changed("year") && inferred.Year != "" {
				resolvedYear = inferred.Year
			}
			if !cmd.Flags().Changed("quest") {
				resolvedQuest = inferred.Quest
			}
			if !cmd.Flags().Changed("part") {
				resolvedPart = inferred.Part
			}
			var missing []string
			if resolvedQuest == "" && !cmd.Flags().Changed("quest") {
				missing = append(missing, "--quest (-q)")
			}
			if resolvedPart == "" && !cmd.Flags().Changed("part") {
				missing = append(missing, "--part (-p)")
			}
			if len(missing) != 0 {
				return fmt.Errorf("%s required: provide explicit flags or run inside a workspace's <year>/quest-NN/part-NN directory; create a missing .ec.json with ec get -y <year> -q <quest> from the intended workspace root", strings.Join(missing, " and "))
			}
			q, err := everybodycodes.ParseQuestFlags(resolvedYear, resolvedQuest)
			if err != nil {
				return err
			}
			part, err := strconv.Atoi(resolvedPart)
			if err != nil || part < 1 || resolvedPart != strconv.Itoa(part) {
				return errors.New("part number must be a positive decimal integer without leading zeros (for example, 1)")
			}
			answer, err := readAnswer(cmd.Context(), args, cmd.InOrStdin())
			if err != nil {
				return err
			}
			return client.SubmitAnswer(cmd.Context(), q, part, answer, root, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVarP(&year, "year", "y", strconv.Itoa(time.Now().Year()), "Quest year (overrides workspace context; otherwise defaults to the current year)")
	cmd.Flags().StringVarP(&questNumber, "quest", "q", "", "Quest number in decimal (for example, 1 or 01)")
	cmd.Flags().StringVarP(&partNumber, "part", "p", "", "Part number in decimal without leading zeros (for example, 1)")
	return cmd
}

func readAnswer(ctx context.Context, args []string, input io.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var answer string
	if len(args) == 1 {
		answer = args[0]
	} else {
		if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			return "", errors.New("provide an answer argument or pipe UTF-8 text into ec submit")
		}
		type result struct {
			data []byte
			err  error
		}
		done := make(chan result, 1)
		go func() {
			data, err := io.ReadAll(input)
			done <- result{data, err}
		}()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case read := <-done:
			if read.err != nil {
				return "", fmt.Errorf("read answer from stdin: %w", read.err)
			}
			answer = string(read.data)
		}
		if strings.HasSuffix(answer, "\n") {
			answer = strings.TrimSuffix(answer, "\n")
			answer = strings.TrimSuffix(answer, "\r")
		}
	}
	if !utf8.ValidString(answer) {
		return "", errors.New("answer must be valid UTF-8 text")
	}
	if strings.TrimSpace(answer) == "" {
		return "", errors.New("answer must not be empty; provide an argument or pipe UTF-8 text into ec submit")
	}
	return answer, nil
}
