package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

const authUsage = "usage: ec auth [check]"

// Retain the HTTP status so callers can distinguish expired sessions from
// temporary server failures without inspecting the text of an error.
type httpStatusError struct {
	endpoint   string
	statusCode int
	status     string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %s", e.endpoint, e.status)
}

func runAuth(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 1 && args[0] == "check" {
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
	if len(args) != 0 {
		return errors.New(authUsage)
	}
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

func saveSessionCookie(value string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	dir := filepath.Join(home, ".config", "everybody-codes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open session directory: %w", err)
	}
	defer root.Close()
	name := ".session-" + rand.Text() + ".tmp"
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer root.Remove(name)
	_, writeErr := io.WriteString(file, value+"\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	if err := root.Rename(name, "session"); err != nil {
		return fmt.Errorf("save session file: %w", err)
	}
	return nil
}
