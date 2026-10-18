package everybodycodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chandlerklein/everybody-codes-cli/internal/session"
)

// SubmitAnswer submits an answer and downloads newly unlocked inputs after success.
func (c *Client) SubmitAnswer(ctx context.Context, q Quest, part int, answer, baseDir string, output io.Writer) error {
	cookie, err := session.Load()
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
	endpoint := fmt.Sprintf("%s/event/%s/quest/%d/part/%d/answer", apiBaseURL, q.Year, q.Number, part)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.AddCookie(cookie)
	resp, err := c.http.Do(req)
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
		statusErr := &HTTPStatusError{Method: http.MethodPost, Endpoint: endpoint, StatusCode: resp.StatusCode, Status: resp.Status, Body: strings.TrimSpace(text)}
		if resp.StatusCode == http.StatusConflict {
			return fmt.Errorf("part already solved: %w", statusErr)
		}
		return statusErr
	}
	if _, err := fmt.Fprintf(output, "Year %s, quest %d, part %d\nHTTP %s\n", q.Year, q.Number, part, resp.Status); err != nil {
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
		if _, err := fmt.Fprintf(output, "Congratulations! You solved part %d of quest %d (%s).\n", part, q.Number, q.Year); err != nil {
			return err
		}
		if err := c.Download(ctx, q, baseDir, false, output); err != nil {
			return fmt.Errorf("answer accepted, but downloading quest inputs failed: %w", err)
		}
	}
	return nil
}
