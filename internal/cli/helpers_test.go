package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

var apiClient = &http.Client{}

const testKey = "0123456789abcdef0123456789abcdef"

// Fixed fixtures generated independently with OpenSSL AES-256-CBC.
var testNotes = []struct {
	plain   string
	encoded string
}{
	{"First line\nSecond line: café\n", "9440069a68fcff18927655d301c4d8cb223bc0327075d19dd1b6f157c25aad17"},
	{"Part two without trailing newline", "a2d11f4488dfc2027d15b27621673550475ebb72f3cf5c6c1f761aca42468c8d4c3f081921abf7418cfc0da5735b4200"},
	{"Third part\n", "ac9927e485737228053c2717390b9d44"},
	{"", "38ae83a3edd22a71ecf296539ddf7320"},
	{"0123456789abcdef", "7dbd88272fb25937692a3b81004741755cc246053c1506c574adf2c40f5cde7e"},
}

const (
	apiBaseURL           = "https://api.everybody.codes"
	maxJSONResponseBytes = 16 << 20
	testURL              = "https://everybody.codes/event/2024/quests/1"
	userURL              = apiBaseURL + "/user/me"
	inputsURL            = "https://everybody.codes/assets/2024/1/input/8.json"
	keysURL              = apiBaseURL + "/event/2024/quest/1"
	testSession          = "test-session-never-log-this"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testAPI struct {
	mu       sync.Mutex
	bodies   map[string]string
	statuses map[string]int
	calls    []string
}

func setup(t *testing.T) *testAPI {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("EVERYBODY_CODES_SESSION_COOKIE", "")
	writeTestFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), testSession+"\n")
	inputs := map[string]string{}
	for i := 0; i < 3; i++ {
		inputs[fmt.Sprint(i+1)] = testNotes[i].encoded
	}
	api := &testAPI{
		bodies: map[string]string{
			userURL:   `{"seed":8}`,
			inputsURL: jsonString(t, inputs),
			keysURL:   jsonString(t, map[string]string{"key1": testKey, "key2": testKey, "key3": testKey}),
		},
		statuses: map[string]int{},
	}
	oldClient := apiClient
	t.Cleanup(func() { apiClient = oldClient })
	apiClient = &http.Client{CheckRedirect: oldClient.CheckRedirect, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		cookie, err := r.Cookie("everybody-codes")
		if err != nil || cookie.Value != testSession {
			t.Error("request did not include the expected session cookie")
		}
		endpoint := r.URL.String()
		api.mu.Lock()
		api.calls = append(api.calls, endpoint)
		api.mu.Unlock()
		body, ok := api.bodies[endpoint]
		if !ok {
			return nil, fmt.Errorf("unexpected request to %s", endpoint)
		}
		status := api.statuses[endpoint]
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	return api
}

func jsonString(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be absent, got %v", path, err)
	}
}

// run uses a development version for command tests.
func run(ctx context.Context, args []string, output io.Writer) error {
	return execute(ctx, args, os.Stdin, output, "dev", everybodycodes.NewClient(apiClient))
}
