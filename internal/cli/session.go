package cli

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func loadSession() (*http.Cookie, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	path := filepath.Join(home, ".config", "everybody-codes", "session")
	value, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read session file %s: %w", path, err)
	}
	session := strings.TrimSpace(string(value))
	source := fmt.Sprintf("session file %s", path)
	if session == "" {
		session = strings.TrimSpace(os.Getenv("EVERYBODY_CODES_SESSION_COOKIE"))
		source = "EVERYBODY_CODES_SESSION_COOKIE"
		if session == "" {
			return nil, fmt.Errorf("session file %s is missing or empty; add your cookie there or set EVERYBODY_CODES_SESSION_COOKIE", path)
		}
	}
	cookie := &http.Cookie{Name: "everybody-codes", Value: session}
	if cookie.Valid() != nil {
		return nil, fmt.Errorf("%s must contain a valid raw cookie value", source)
	}
	return cookie, nil
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
