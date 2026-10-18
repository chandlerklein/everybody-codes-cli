package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"
)

func runAuthCheck(ctx context.Context, output io.Writer) error {
	cookie, err := loadSession()
	if err != nil {
		return fmt.Errorf("%w; run ec auth to save a session cookie", err)
	}
	if err := checkSession(ctx, cookie); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "Session is valid.")
	return err
}

func runAuth(ctx context.Context, input io.Reader, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprint(output, "Session cookie: "); err != nil {
		return err
	}
	value, err := readSessionCookie(ctx, input)
	if _, newlineErr := fmt.Fprintln(output); err == nil {
		err = newlineErr
	}
	if err != nil {
		return fmt.Errorf("read session cookie: %w", err)
	}
	value = strings.TrimSpace(value)
	cookie := &http.Cookie{Name: "everybody-codes", Value: value}
	if value == "" || cookie.Valid() != nil {
		return errors.New("enter a valid raw session cookie value")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := saveSessionCookie(value); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "Session saved. Run ec auth check to verify it.")
	return err
}

func checkSession(ctx context.Context, cookie *http.Cookie) error {
	var user struct {
		Seed *int64 `json:"seed"`
	}
	if err := getJSON(ctx, apiBaseURL+"/user/me", cookie, &user); err != nil {
		var status *httpStatusError
		if errors.As(err, &status) && (status.statusCode == http.StatusUnauthorized || status.statusCode == http.StatusForbidden) {
			return fmt.Errorf("session authentication failed (HTTP %d); run ec auth to replace the cookie", status.statusCode)
		}
		return fmt.Errorf("could not verify session: %w", err)
	}
	if user.Seed == nil || *user.Seed < 0 {
		return errors.New("could not verify session: user response must contain a nonnegative integer seed")
	}
	return nil
}

func readSessionCookie(ctx context.Context, input io.Reader) (string, error) {
	terminal := false
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fd := int(file.Fd())
		state, err := term.MakeRaw(fd)
		if err != nil {
			return "", err
		}
		// Set the mode before starting the reader, so cancellation always
		// restores echo even when the input goroutine is still blocked.
		defer term.Restore(fd, state)
		terminal = true
	}
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := readCookieLine(input, terminal)
		done <- result{value, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-done:
		return result.value, result.err
	}
}

func readCookieLine(input io.Reader, terminal bool) (string, error) {
	reader := bufio.NewReader(input)
	var value []byte
	for {
		ch, err := reader.ReadByte()
		if errors.Is(err, io.EOF) && len(value) != 0 {
			return string(value), nil
		}
		if err != nil {
			return "", err
		}
		if ch == '\n' || ch == '\r' {
			return string(value), nil
		}
		if terminal {
			switch ch {
			case 3: // Ctrl-C interrupts raw terminal input.
				return "", context.Canceled
			case 4: // Ctrl-D ends input.
				return "", io.EOF
			case 8, 127: // Backspace / delete.
				if len(value) != 0 {
					value = value[:len(value)-1]
				}
				continue
			case 21: // Ctrl-U clears the line.
				value = value[:0]
				continue
			}
		}
		if len(value) >= 16<<10 {
			return "", errors.New("session cookie is too long")
		}
		value = append(value, ch)
	}
}
