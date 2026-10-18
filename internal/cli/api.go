package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	apiBaseURL           = "https://api.everybody.codes"
	maxJSONResponseBytes = 16 << 20
)

var apiClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Retain the HTTP status so callers can distinguish expired sessions from
// temporary server failures without inspecting the text of an error.
type httpStatusError struct {
	method     string
	endpoint   string
	statusCode int
	status     string
	body       string
}

func (e *httpStatusError) Error() string {
	message := fmt.Sprintf("%s %s: HTTP %s", e.method, e.endpoint, e.status)
	if e.body != "" {
		message += ": " + e.body
	}
	return message
}

func questDownloadError(q quest, action string, err error) error {
	if questUnavailable(err) {
		return fmt.Errorf("%s: year %s or quest %d does not exist or is not available yet: %w", action, q.year, q.number, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func questUnavailable(err error) bool {
	var status *httpStatusError
	return errors.As(err, &status) && (status.statusCode == http.StatusNotFound || status.statusCode == http.StatusUnprocessableEntity)
}

func getJSON(ctx context.Context, endpoint string, cookie *http.Cookie, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.AddCookie(cookie)
	resp, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{method: http.MethodGet, endpoint: endpoint, statusCode: resp.StatusCode, status: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read JSON from %s: %w", endpoint, err)
	}
	if len(body) > maxJSONResponseBytes {
		return fmt.Errorf("JSON response from %s exceeds %d bytes", endpoint, maxJSONResponseBytes)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("invalid JSON from %s: %w", endpoint, err)
	}
	return nil
}
