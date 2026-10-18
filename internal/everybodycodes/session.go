package everybodycodes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// CheckSession verifies that the cookie authenticates a user with a valid seed.
func (c *Client) CheckSession(ctx context.Context, cookie *http.Cookie) error {
	var user struct {
		Seed *int64 `json:"seed"`
	}
	if err := c.getJSON(ctx, apiBaseURL+"/user/me", cookie, &user); err != nil {
		var status *HTTPStatusError
		if errors.As(err, &status) && (status.StatusCode == http.StatusUnauthorized || status.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("session authentication failed (HTTP %d); run ec auth to replace the cookie", status.StatusCode)
		}
		return fmt.Errorf("could not verify session: %w", err)
	}
	if user.Seed == nil || *user.Seed < 0 {
		return errors.New("could not verify session: user response must contain a nonnegative integer seed")
	}
	return nil
}
