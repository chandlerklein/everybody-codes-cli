package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionFile(t *testing.T) {
	for _, tt := range []struct {
		name, value string
		missing     bool
	}{
		{"missing", "", true},
		{"empty", " \n", false},
		{"invalid", "sensitive;cookie", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			path := filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")
			if tt.missing {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, path, tt.value)
			}
			err := run(context.Background(), []string{"get", testURL}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "session file") {
				t.Fatalf("error = %v, want session file error", err)
			}
			if tt.value != "" && strings.Contains(err.Error(), tt.value) {
				t.Error("error exposed session contents")
			}
			if len(api.calls) != 0 {
				t.Error("invalid session caused HTTP requests")
			}
		})
	}
}

func TestSessionCookieFallback(t *testing.T) {
	for _, tt := range []struct {
		name, file, env, wantErr string
		missing                  bool
	}{
		{"file takes precedence", testSession, "different-environment-cookie", "", false},
		{"missing file", "", testSession, "", true},
		{"empty file", "", testSession, "", false},
		{"whitespace file", " \n\t", testSession, "", false},
		{"trim environment value", "", " \t" + testSession + "\n", "", true},
		{"both missing", "", "", "missing or empty", true},
		{"both whitespace", " \n", " \t", "missing or empty", false},
		{"invalid environment cookie", "", "sensitive;environment-cookie", "EVERYBODY_CODES_SESSION_COOKIE must contain a valid raw cookie value", true},
		{"invalid file takes precedence", "sensitive;file-cookie", testSession, "must contain a valid raw cookie value", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			t.Setenv("EVERYBODY_CODES_SESSION_COOKIE", tt.env)
			path := filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")
			if tt.missing {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, path, tt.file)
			}
			var output bytes.Buffer
			err := run(context.Background(), []string{"get", testURL}, &output)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(api.calls) != 3 {
					t.Errorf("made %d requests, want 3", len(api.calls))
				}
				assertFile(t, "2024/quest-01/part-01/input.txt", testNotes[0].plain)
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if len(api.calls) != 0 {
					t.Error("invalid credentials caused HTTP requests")
				}
			}
			logged := output.String()
			if err != nil {
				logged += err.Error()
			}
			for _, value := range []string{tt.file, tt.env} {
				if secret := strings.TrimSpace(value); secret != "" && strings.Contains(logged, secret) {
					t.Error("session cookie was exposed")
				}
			}
		})
	}
}
