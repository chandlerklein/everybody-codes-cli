package everybodycodes

import (
	"fmt"
	"io"
	"net/http"
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
				client := NewClient(&http.Client{})
				calls := 0
				client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
				err := client.getJSON(t.Context(), userURL, cookie, &result)
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
			client := NewClient(&http.Client{})
			const jsonBody = `{"ok":true}`
			body := jsonBody + strings.Repeat(" ", size-len(jsonBody))
			reader := strings.NewReader(body)
			client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1, // Enforce the limit even without a Content-Length header.
					Body:          io.NopCloser(reader),
					Header:        make(http.Header),
				}, nil
			})
			cookie := &http.Cookie{Name: "everybody-codes", Value: testSession}
			var result map[string]bool
			err := client.getJSON(t.Context(), userURL, cookie, &result)
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
