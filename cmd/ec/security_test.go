package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetJSONRejectsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, destination := range []string{
			apiBaseURL + "/redirected",
			"http://api.everybody.codes/redirected",
			"https://untrusted.api.everybody.codes/redirected",
			"https://unrelated.example/redirected",
		} {
			t.Run(fmt.Sprintf("%d/%s", status, destination), func(t *testing.T) {
				setup(t)
				calls := 0
				apiClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls > 1 {
						t.Error("followed redirect with an authenticated request")
					}
					return &http.Response{
						StatusCode: status,
						Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
						Header:     http.Header{"Location": {destination}},
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				})
				cookie := &http.Cookie{Name: "everybody-codes", Value: testSession}
				var result any
				err := getJSON(t.Context(), userURL, cookie, &result)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
					t.Fatalf("error = %v, want HTTP %d", err, status)
				}
				if calls != 1 {
					t.Fatalf("made %d requests, want 1", calls)
				}
				if strings.Contains(err.Error(), testSession) {
					t.Fatal("error exposed the session cookie")
				}
			})
		}
	}
}

func TestGetJSONResponseLimit(t *testing.T) {
	for _, size := range []int{maxJSONResponseBytes - 1, maxJSONResponseBytes, maxJSONResponseBytes + 1, maxJSONResponseBytes + 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			setup(t)
			const jsonBody = `{"ok":true}`
			body := jsonBody + strings.Repeat(" ", size-len(jsonBody))
			reader := strings.NewReader(body)
			apiClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1, // Enforce the limit even without a Content-Length header.
					Body:          io.NopCloser(reader),
					Header:        make(http.Header),
				}, nil
			})
			cookie := &http.Cookie{Name: "everybody-codes", Value: testSession}
			var result map[string]bool
			err := getJSON(t.Context(), userURL, cookie, &result)
			if size > maxJSONResponseBytes {
				if err == nil || !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("error = %v, want response size error", err)
				}
				if result != nil {
					t.Fatal("oversized response was decoded")
				}
				if read := size - reader.Len(); read > maxJSONResponseBytes+1 {
					t.Fatalf("read %d bytes from an oversized response", read)
				}
				return
			}
			if err != nil || !result["ok"] {
				t.Fatalf("result = %v, error = %v", result, err)
			}
		})
	}
}

func TestInitQuestIgnoresEncodedLinks(t *testing.T) {
	for _, kind := range []string{"external symlink", "internal symlink", "hard link"} {
		t.Run(kind, func(t *testing.T) {
			setup(t)
			dir := "2024/quest-01/part-01"
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "unrelated.txt")
			linkTarget := target
			if kind == "internal symlink" {
				target = "2024/quest-01/unrelated.txt"
				linkTarget = "../unrelated.txt"
			}
			writeTestFile(t, target, "keep this file")
			path := filepath.Join(dir, "input-encoded.txt")
			link := os.Symlink
			if kind == "hard link" {
				link = os.Link
			}
			if err := link(linkTarget, path); err != nil {
				t.Fatal(err)
			}
			if err := run(context.Background(), []string{"get", testURL}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertFile(t, target, "keep this file")
			assertFile(t, path, "keep this file")
			assertFile(t, filepath.Join(dir, "input.txt"), testNotes[0].plain)
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "hard link" && info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("legacy encoded symlink was replaced")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Errorf("part directory has %d entries, want 2 (no temporary files)", len(entries))
			}
		})
	}
}

func TestInitQuestRejectsDirectoryEscape(t *testing.T) {
	for _, path := range []string{
		"2024",
		"2024/quest-01",
		"2024/quest-01/part-01",
	} {
		t.Run(path, func(t *testing.T) {
			api := setup(t)
			outside := t.TempDir()
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if err := run(context.Background(), []string{"get", testURL}, io.Discard); err == nil {
				t.Fatal("accepted a directory symlink escaping the output root")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("modified the directory outside the root: entries = %v, error = %v", entries, err)
			}
			if len(api.calls) != 0 {
				t.Fatal("made HTTP requests before rejecting the directory escape")
			}
		})
	}
}
