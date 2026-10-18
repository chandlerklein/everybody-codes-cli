package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSubmitCommand() *cobra.Command {
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
			q, err := parseQuestFlags(year, questNumber)
			if err != nil {
				return err
			}
			part, err := strconv.Atoi(partNumber)
			if err != nil || part < 1 || !decimalQuestNumber.MatchString(partNumber) || partNumber[0] == '0' {
				return errors.New("part number must be a positive decimal integer without leading zeros (for example, 1)")
			}
			answer, err := readAnswer(cmd.Context(), args, cmd.InOrStdin())
			if err != nil {
				return err
			}
			return submitAnswer(cmd.Context(), q, part, answer, cmd.OutOrStdout())
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

func submitAnswer(ctx context.Context, q quest, part int, answer string, output io.Writer) error {
	cookie, err := loadSession()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Answer string `json:"answer"`
	}{Answer: answer})
	if err != nil {
		return fmt.Errorf("encode answer: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf("%s/event/%s/quest/%d/part/%d/answer", apiBaseURL, q.year, q.number, part)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.AddCookie(cookie)
	resp, err := apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("submit answer: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read submission response (HTTP %s): %w", resp.Status, err)
	}
	if len(body) > maxJSONResponseBytes {
		return fmt.Errorf("submission response (HTTP %s) exceeds %d bytes", resp.Status, maxJSONResponseBytes)
	}
	// Responses can contain arbitrary API errors; never echo the session cookie.
	text := strings.ReplaceAll(string(body), cookie.Value, "[REDACTED]")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := &httpStatusError{method: http.MethodPost, endpoint: endpoint, statusCode: resp.StatusCode, status: resp.Status, body: strings.TrimSpace(text)}
		if resp.StatusCode == http.StatusConflict {
			return fmt.Errorf("part already solved: %w", statusErr)
		}
		return statusErr
	}
	if _, err := fmt.Fprintf(output, "HTTP %s\n", resp.Status); err != nil {
		return err
	}
	if text != "" {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if _, err := io.WriteString(output, text); err != nil {
			return err
		}
	}
	var result struct {
		Correct bool `json:"correct"`
	}
	if json.Unmarshal(body, &result) == nil && result.Correct {
		if _, err := fmt.Fprintf(output, "Congratulations! You solved part %d of quest %d (%s).\n", part, q.number, q.year); err != nil {
			return err
		}
		if err := initQuest(ctx, q, ".", false, output); err != nil {
			return fmt.Errorf("answer accepted, but downloading quest inputs failed: %w", err)
		}
	}
	return nil
}
