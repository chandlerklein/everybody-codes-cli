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
		Use:   "submit --quest <quest> --part <part> [<ANSWER>]",
		Short: "Submit an Everybody Codes answer",
		Long: "Submit a UTF-8 answer as an argument or, when omitted, read it from standard input. " +
			"One final LF or CRLF is removed from standard input; other whitespace is preserved. " +
			"Quest numbers may have leading zeros. Part numbers must be positive decimal integers without leading zeros. " +
			"The year defaults to the current year. Correct answers automatically download newly unlocked inputs into the current directory.",
		Example: "  ec submit -y 2024 -q 1 -p 1 1309\n  ec submit -q 1 -p 1 'text answer'\n  ./solve | ec submit -y 2024 -q 1 -p 1",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := everybodycodes.ParseQuestFlags(year, questNumber)
			if err != nil {
				return err
			}
			part, err := strconv.Atoi(partNumber)
			if err != nil || part < 1 || partNumber != strconv.Itoa(part) {
				return errors.New("part number must be a positive decimal integer without leading zeros (for example, 1)")
			}
			answer, err := readAnswer(cmd.Context(), args, cmd.InOrStdin())
			if err != nil {
				return err
			}
			return client.SubmitAnswer(cmd.Context(), q, part, answer, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVarP(&year, "year", "y", strconv.Itoa(time.Now().Year()), "Quest year (defaults to the current year)")
	cmd.Flags().StringVarP(&questNumber, "quest", "q", "", "Quest number in decimal (for example, 1 or 01)")
	cmd.Flags().StringVarP(&partNumber, "part", "p", "", "Part number in decimal without leading zeros (for example, 1)")
	_ = cmd.MarkFlagRequired("quest")
	_ = cmd.MarkFlagRequired("part")
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
