// Package everybodycodes manages quests, answers, and locally saved inputs.
package everybodycodes

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

// Client performs authenticated Everybody Codes requests.
type Client struct {
	http *http.Client
}

// NewClient copies client (or uses default HTTP settings when nil) and disables
// redirects so authenticated requests cannot forward the session cookie.
func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	configured := *client
	configured.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{http: &configured}
}

// HTTPStatusError preserves the request and response details of a failed API call.
type HTTPStatusError struct {
	Method     string
	Endpoint   string
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	message := fmt.Sprintf("%s %s: HTTP %s", e.Method, e.Endpoint, e.Status)
	if e.Body != "" {
		message += ": " + e.Body
	}
	return message
}

func questDownloadError(q Quest, action string, err error) error {
	if questUnavailable(err) {
		return fmt.Errorf("%s: year %s or quest %d does not exist or is not available yet: %w", action, q.Year, q.Number, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func questUnavailable(err error) bool {
	var status *HTTPStatusError
	return errors.As(err, &status) && (status.StatusCode == http.StatusNotFound || status.StatusCode == http.StatusUnprocessableEntity)
}

func (c *Client) getJSON(ctx context.Context, endpoint string, cookie *http.Cookie, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.AddCookie(cookie)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{Method: http.MethodGet, Endpoint: endpoint, StatusCode: resp.StatusCode, Status: resp.Status}
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
