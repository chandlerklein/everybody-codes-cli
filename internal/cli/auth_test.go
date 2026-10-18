package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthSavesSession(t *testing.T) {
	api := setup(t)
	path := filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runAuth(t.Context(), strings.NewReader(" replacement-cookie \r\n"), &output); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "replacement-cookie\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("session permissions = %o, want 600", info.Mode().Perm())
	}
	if strings.Contains(output.String(), "replacement-cookie") {
		t.Fatal("output contains cookie value")
	}
	if !strings.Contains(output.String(), "Session saved") {
		t.Errorf("output = %q", output.String())
	}
	if len(api.calls) != 0 {
		t.Fatalf("saving a cookie made network requests: %v", api.calls)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary session files remain: %v, %v", entries, err)
	}
}

func TestAuthCreatesSessionDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runAuth(t.Context(), strings.NewReader(testSession+"\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")
	assertFile(t, path, testSession+"\n")
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("session directory permissions: info = %v, error = %v", info, err)
	}
}

func TestAuthReplacesSessionLinksWithoutChangingTarget(t *testing.T) {
	for _, kind := range []string{"external symlink", "internal symlink", "hard link"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, ".config", "everybody-codes")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "original")
			if kind == "internal symlink" {
				target = filepath.Join(dir, "original")
			}
			writeTestFile(t, target, "preserve original")
			path := filepath.Join(dir, "session")
			var err error
			if kind == "hard link" {
				err = os.Link(target, path)
			} else if kind == "internal symlink" {
				err = os.Symlink("original", path)
			} else {
				err = os.Symlink(target, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := saveSessionCookie("replacement-cookie"); err != nil {
				t.Fatal(err)
			}
			assertFile(t, target, "preserve original")
			assertFile(t, path, "replacement-cookie\n")
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatalf("session is not a regular file with mode 0600: %v, %v", info, err)
			}
		})
	}
}

func TestAuthRejectsInvalidInputWithoutReplacingSession(t *testing.T) {
	for _, value := range []string{"", "\n", "   \n", "cookie;other=value\n", "cookie\tvalue\n", strings.Repeat("x", (16<<10)+1)} {
		t.Run(value[:min(len(value), 20)], func(t *testing.T) {
			setup(t)
			var output bytes.Buffer
			if err := runAuth(t.Context(), strings.NewReader(value), &output); err == nil {
				t.Fatal("accepted invalid cookie")
			}
			assertFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), testSession+"\n")
		})
	}
}

func TestAuthCheck(t *testing.T) {
	for _, seed := range []string{"0", "8", "9223372036854775807"} {
		t.Run(seed, func(t *testing.T) {
			api := setup(t)
			api.bodies[userURL] = `{"seed":` + seed + `}`
			var output bytes.Buffer
			if err := runAuthCheck(t.Context(), &output); err != nil {
				t.Fatal(err)
			}
			if output.String() != "Session is valid.\n" {
				t.Errorf("output = %q", output.String())
			}
			if len(api.calls) != 1 || api.calls[0] != userURL {
				t.Errorf("requests = %v, want user endpoint only", api.calls)
			}
			if _, err := os.Stat("2024"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("auth check created quest directories")
			}
		})
	}
}

func TestRunAuthCheck(t *testing.T) {
	api := setup(t)
	var output bytes.Buffer
	if err := run(t.Context(), []string{"auth", "check"}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Session is valid.\n" || len(api.calls) != 1 || api.calls[0] != userURL {
		t.Fatalf("output = %q, requests = %v", output.String(), api.calls)
	}
}

func TestAuthCheckFailure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"unauthorized", 401, `{}`, "run ec auth to replace the cookie"},
		{"forbidden", 403, `{}`, "run ec auth to replace the cookie"},
		{"server error", 503, `{}`, "could not verify session"},
		{"missing seed", 200, `{}`, "nonnegative integer seed"},
		{"null seed", 200, `{"seed":null}`, "nonnegative integer seed"},
		{"negative seed", 200, `{"seed":-1}`, "nonnegative integer seed"},
		{"wrong seed type", 200, `{"seed":"8"}`, "could not verify session"},
		{"noninteger seed", 200, `{"seed":8.5}`, "could not verify session"},
		{"invalid json", 200, `<html>unavailable</html>`, "could not verify session"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			api.statuses[userURL] = tt.status
			api.bodies[userURL] = tt.body
			var output bytes.Buffer
			err := runAuthCheck(t.Context(), &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %s", err, tt.want)
			}
			if strings.Contains(err.Error(), testSession) || strings.Contains(output.String(), testSession) {
				t.Fatal("auth output exposed the cookie")
			}
			if tt.status != 401 && tt.status != 403 && strings.Contains(err.Error(), "replace the cookie") {
				t.Fatal("reported an expired session for an inconclusive check")
			}
			if output.Len() != 0 {
				t.Fatal("printed success despite failed verification")
			}
		})
	}
}

func TestAuthCheckNetworkFailure(t *testing.T) {
	setup(t)
	apiClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})
	err := runAuthCheck(t.Context(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not verify session") || strings.Contains(err.Error(), "replace") {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthCheckUsesSessionFallback(t *testing.T) {
	setup(t)
	if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERYBODY_CODES_SESSION_COOKIE", testSession)
	if err := runAuthCheck(t.Context(), io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERYBODY_CODES_SESSION_COOKIE", "")
	err := runAuthCheck(t.Context(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "run ec auth") {
		t.Fatalf("error = %v, want setup instructions", err)
	}
}

func TestReadCookieLineTerminalEditing(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"ab\bc\n", "ac"},
		{"ab\x7fc\r", "ac"},
		{"old\x15new\n", "new"},
		{"\bvalue\n", "value"},
	} {
		got, err := readCookieLine(strings.NewReader(tt.input), true)
		if err != nil || got != tt.want {
			t.Errorf("got %q, %v; want %q", got, err, tt.want)
		}
	}
	if _, err := readCookieLine(strings.NewReader("value\x03"), true); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ctrl-C error = %v", err)
	}
}

func TestAuthCancellation(t *testing.T) {
	setup(t)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runAuth(ctx, reader, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	assertFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), testSession+"\n")
}
