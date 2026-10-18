package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testURL     = "https://everybody.codes/event/2024/quests/1"
	userURL     = apiBaseURL + "/user/me"
	inputsURL   = "https://everybody.codes/assets/2024/1/input/8.json"
	keysURL     = apiBaseURL + "/event/2024/quest/1"
	testSession = "test-session-never-log-this"
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

func TestInitQuest(t *testing.T) {
	api := setup(t)
	// Existing directories must not prevent initialization.
	if err := os.MkdirAll("2024/quest-01/part-02/input", 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{testURL}, &output); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-01/part-%02d/input", part)
		assertFile(t, filepath.Join(dir, "input-encoded.txt"), testNotes[part-1].encoded)
		assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
	}
	wantCalls := []string{userURL, inputsURL, keysURL}
	slices.Sort(api.calls)
	slices.Sort(wantCalls)
	if !slices.Equal(api.calls, wantCalls) {
		t.Errorf("requests = %v, want %v", api.calls, wantCalls)
	}
	if strings.Contains(output.String(), testSession) || strings.Contains(output.String(), testKey) {
		t.Fatal("output contains session or AES key")
	}
	// A complete quest works offline, even without the session file.
	if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{testURL}, &output); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 3 {
		t.Error("complete quest made additional HTTP requests")
	}
}

func TestInitQuestFetchesKeysAlongsideSeedAndInputs(t *testing.T) {
	setup(t)
	transport := apiClient.Transport
	inputsStarted := make(chan struct{})
	keysStarted := make(chan struct{})
	apiClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var peerStarted <-chan struct{}
		switch r.URL.String() {
		case userURL:
			peerStarted = keysStarted
		case inputsURL:
			close(inputsStarted)
			peerStarted = keysStarted
		case keysURL:
			close(keysStarted)
			peerStarted = inputsStarted
		default:
			return transport.RoundTrip(r)
		}
		// The seed waits for keys to start; keys and inputs wait for each other.
		select {
		case <-peerStarted:
			return transport.RoundTrip(r)
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := run(ctx, []string{testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input/input.txt", part), testNotes[part-1].plain)
	}
}

func TestInitQuestCancelsKeysOnFailure(t *testing.T) {
	for _, tt := range []struct {
		name, endpoint, want string
	}{
		{"seed", userURL, "get user seed"},
		{"inputs", inputsURL, "get encoded notes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			api.statuses[tt.endpoint] = http.StatusNotFound
			transport := apiClient.Transport
			keysStarted := make(chan struct{})
			keysCanceled := make(chan struct{})
			apiClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case keysURL:
					close(keysStarted)
					<-r.Context().Done()
					close(keysCanceled)
					return nil, r.Context().Err()
				case tt.endpoint:
					select {
					case <-keysStarted:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				return transport.RoundTrip(r)
			})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := run(ctx, []string{testURL}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "HTTP 404") {
				t.Fatalf("error = %v, want %s HTTP 404", err, tt.want)
			}
			if ctx.Err() != nil {
				t.Fatal("pending key request was not canceled before the test timeout")
			}
			select {
			case <-keysCanceled:
			default:
				t.Fatal("returned before the pending key request was canceled")
			}
		})
	}
}

func TestInitQuestUnlocksPartsOnRerun(t *testing.T) {
	api := setup(t)
	api.bodies[keysURL] = jsonString(t, map[string]any{"key1": testKey, "key2": nil})
	if err := run(context.Background(), []string{testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for part := 2; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-01/part-%02d/input", part)
		assertFile(t, filepath.Join(dir, "input-encoded.txt"), testNotes[part-1].encoded)
		assertMissing(t, filepath.Join(dir, "input.txt"))
	}
	writeTestFile(t, "2024/quest-01/part-01/input/input.txt", "keep my existing notes")
	// Completed parts are skipped even if their remote input and key are unusable.
	api.bodies[inputsURL] = jsonString(t, map[string]string{"2": testNotes[1].encoded, "3": testNotes[2].encoded})
	api.bodies[keysURL] = jsonString(t, map[string]any{"key1": 42, "key2": testKey, "key3": testKey})
	api.calls = nil
	delete(api.bodies, userURL)
	delete(api.bodies, inputsURL)
	if err := run(context.Background(), []string{testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(api.calls, []string{keysURL}) {
		t.Errorf("requests = %v, want only quest keys", api.calls)
	}
	assertFile(t, "2024/quest-01/part-01/input/input.txt", "keep my existing notes")
	assertFile(t, "2024/quest-01/part-01/input/input-encoded.txt", testNotes[0].encoded)
	for part := 2; part <= 3; part++ {
		assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input/input.txt", part), testNotes[part-1].plain)
	}
}

func TestInitQuestFetchesOnlyMissingEncodedNotes(t *testing.T) {
	api := setup(t)
	// Only part 2 is missing ciphertext. Its response need not include cached
	// part 1 or the completed part 3, and existing cache content stays intact.
	cachePath := "2024/quest-01/part-01/input/input-encoded.txt"
	writeTestFile(t, cachePath, testNotes[0].encoded+"\n")
	writeTestFile(t, "2024/quest-01/part-03/input/input.txt", "existing notes")
	api.bodies[inputsURL] = jsonString(t, map[string]string{"2": testNotes[1].encoded})
	if err := run(context.Background(), []string{testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertFile(t, cachePath, testNotes[0].encoded+"\n")
	assertFile(t, "2024/quest-01/part-01/input/input.txt", testNotes[0].plain)
	assertFile(t, "2024/quest-01/part-02/input/input-encoded.txt", testNotes[1].encoded)
	assertFile(t, "2024/quest-01/part-02/input/input.txt", testNotes[1].plain)
	assertFile(t, "2024/quest-01/part-03/input/input.txt", "existing notes")
	wantCalls := []string{userURL, inputsURL, keysURL}
	slices.Sort(api.calls)
	slices.Sort(wantCalls)
	if !slices.Equal(api.calls, wantCalls) {
		t.Errorf("requests = %v, want %v", api.calls, wantCalls)
	}
}

func TestInitQuestRefreshesMalformedCache(t *testing.T) {
	for _, tt := range []struct {
		name, encoded string
	}{
		{"empty", ""},
		{"whitespace", " \n\t"},
		{"not hex", "invalid ciphertext"},
		{"incomplete AES block", "deadbeef"},
		{"oversized", strings.Repeat("0", maxJSONResponseBytes+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			for part := 1; part <= 3; part++ {
				encoded := testNotes[part-1].encoded
				if part == 1 {
					encoded = tt.encoded
				}
				writeTestFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input/input-encoded.txt", part), encoded)
			}
			api.bodies[inputsURL] = jsonString(t, map[string]string{"1": testNotes[0].encoded})
			if err := run(context.Background(), []string{testURL}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertFile(t, "2024/quest-01/part-01/input/input-encoded.txt", testNotes[0].encoded)
			for part := 1; part <= 3; part++ {
				assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input/input.txt", part), testNotes[part-1].plain)
			}
		})
	}
}

func TestInitQuestReportsUnreadableCache(t *testing.T) {
	api := setup(t)
	// A directory is a portable way to exercise a read failure, including
	// when tests run as a user who can bypass ordinary file permissions.
	path := "2024/quest-01/part-01/input/input-encoded.txt"
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{testURL}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read encoded notes for part 1") {
		t.Fatalf("error = %v, want cache read failure", err)
	}
	if len(api.calls) != 0 {
		t.Errorf("unreadable cache caused HTTP requests: %v", api.calls)
	}
}

func TestInitQuestCachedInputsStillRequireKeys(t *testing.T) {
	api := setup(t)
	for part := 1; part <= 3; part++ {
		writeTestFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input/input-encoded.txt", part), testNotes[part-1].encoded)
	}
	api.statuses[keysURL] = http.StatusUnauthorized
	err := run(context.Background(), []string{testURL}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "get quest keys") || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error = %v, want quest keys HTTP 401", err)
	}
	if !slices.Equal(api.calls, []string{keysURL}) {
		t.Errorf("requests = %v, want only quest keys", api.calls)
	}
	assertMissing(t, "2024/quest-01/part-01/input/input.txt")
}

func TestInitQuestTwoDigitNumber(t *testing.T) {
	api := setup(t)
	inputEndpoint := "https://everybody.codes/assets/2024/12/input/8.json"
	keyEndpoint := apiBaseURL + "/event/2024/quest/12"
	api.bodies[inputEndpoint] = api.bodies[inputsURL]
	api.bodies[keyEndpoint] = api.bodies[keysURL]
	delete(api.bodies, inputsURL)
	delete(api.bodies, keysURL)
	if err := run(context.Background(), []string{"https://everybody.codes/event/2024/quests/12"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-12/part-%02d/input", part)
		assertFile(t, filepath.Join(dir, "input-encoded.txt"), testNotes[part-1].encoded)
		assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
	}
	wantCalls := []string{userURL, inputEndpoint, keyEndpoint}
	slices.Sort(api.calls)
	slices.Sort(wantCalls)
	if !slices.Equal(api.calls, wantCalls) {
		t.Errorf("unexpected requests: %v", api.calls)
	}
}

func TestInitQuestDirectoryFlag(t *testing.T) {
	absoluteDir := filepath.Join(t.TempDir(), "new destination")
	for _, tt := range []struct {
		name, dir string
		args      []string
	}{
		{"before URL", "elsewhere", []string{"--dir", "elsewhere", testURL}},
		{"after URL", "elsewhere", []string{testURL, "--dir", "elsewhere"}},
		{"equals syntax", "nested/destination", []string{"--dir=nested/destination", testURL}},
		{"space in path", "my solutions", []string{testURL, "--dir=my solutions"}},
		{"absolute path", absoluteDir, []string{"--dir", absoluteDir, testURL}},
		{"end of options", "elsewhere", []string{"--dir", "elsewhere", "--", testURL}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := run(context.Background(), tt.args, io.Discard); err != nil {
				t.Fatal(err)
			}
			for part := 1; part <= 3; part++ {
				dir := filepath.Join(tt.dir, fmt.Sprintf("2024/quest-01/part-%02d/input", part))
				assertFile(t, filepath.Join(dir, "input-encoded.txt"), testNotes[part-1].encoded)
				assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
			}
			assertMissing(t, "2024")
			if got, err := os.Getwd(); err != nil || got != cwd {
				t.Fatalf("working directory changed: got %q (%v), want %q", got, err, cwd)
			}
			path := filepath.Join(tt.dir, "2024/quest-01/part-01/input/input.txt")
			writeTestFile(t, path, "preserve these inputs")
			api.calls = nil
			if err := run(context.Background(), tt.args, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertFile(t, path, "preserve these inputs")
			if len(api.calls) != 0 {
				t.Error("complete quest in the selected directory made HTTP requests")
			}
		})
	}
}

func TestRunInvalidDirectoryFlag(t *testing.T) {
	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"missing value", "requires a directory", []string{testURL, "--dir"}},
		{"empty value", "requires a nonempty directory", []string{"--dir", "", testURL}},
		{"empty equals value", "requires a nonempty directory", []string{testURL, "--dir="}},
		{"unknown option", "unknown option", []string{"--directory", "elsewhere", testURL}},
		{"missing URL", usage, []string{"--dir", "elsewhere"}},
		{"flag after end of options", usage, []string{"--", testURL, "--dir", "elsewhere"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			err := run(context.Background(), tt.args, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if len(api.calls) != 0 {
				t.Error("invalid arguments caused HTTP requests")
			}
		})
	}
}

func TestInitQuestFileConflicts(t *testing.T) {
	for _, tt := range []struct {
		name, path, want string
		directory        bool
	}{
		{"event is a file", "2024", "create input directory", false},
		{"notes is a directory", "2024/quest-01/part-01/input/input.txt", "must be a regular file", true},
		{"encoded notes is a directory", "2024/quest-01/part-01/input/input-encoded.txt", "read encoded notes", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setup(t)
			if tt.directory {
				if err := os.MkdirAll(tt.path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, tt.path, "existing file")
			}
			err := run(context.Background(), []string{testURL}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if !tt.directory {
				assertFile(t, tt.path, "existing file")
			}
		})
	}
}

func TestWriteNotesPreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	writeTestFile(t, filepath.Join(dir, "input.txt"), "existing notes")
	if err := writeNotes(root, "input.txt", []byte("replacement notes")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dir, "input.txt"), "existing notes")
}

func TestInitQuestFailures(t *testing.T) {
	for _, tt := range []struct {
		name, endpoint, body, want string
		status                     int
	}{
		{"unauthorized user", userURL, testSession, "HTTP 401", 401},
		{"input unavailable", inputsURL, "", "HTTP 404", 404},
		{"keys unavailable", keysURL, "", "HTTP 503", 503},
		{"malformed JSON", userURL, "<html>", "invalid JSON", 0},
		{"missing seed", userURL, `{}`, "seed", 0},
		{"null seed", userURL, `{"seed":null}`, "seed", 0},
		{"negative seed", userURL, `{"seed":-1}`, "seed", 0},
		{"fractional seed", userURL, `{"seed":1.5}`, "invalid JSON", 0},
		{"missing input", inputsURL, `{}`, "missing encoded notes", 0},
		{"invalid key type", keysURL, `{"key1":123}`, "key1 must be a string or null", 0},
		{"invalid key", keysURL, `{"key1":"short"}`, "invalid AES key length", 0},
		{"bad encoded notes", inputsURL, `{"1":"bad","2":"bad","3":"bad"}`, "hexadecimal", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			api.bodies[tt.endpoint] = tt.body
			api.statuses[tt.endpoint] = tt.status
			var output bytes.Buffer
			err := run(context.Background(), []string{testURL}, &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error()+output.String(), testSession) {
				t.Error("session cookie was exposed")
			}
			assertMissing(t, "2024/quest-01/part-01/input/input.txt")
		})
	}
}

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
			err := run(context.Background(), []string{testURL}, io.Discard)
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
			err := run(context.Background(), []string{testURL}, &output)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(api.calls) != 3 {
					t.Errorf("made %d requests, want 3", len(api.calls))
				}
				assertFile(t, "2024/quest-01/part-01/input/input.txt", testNotes[0].plain)
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

func TestParseQuestURL(t *testing.T) {
	for _, tt := range []struct {
		url, event string
		number     int
	}{
		{testURL, "2024", 1},
		{"https://everybody.codes/event/2024/quests/12/", "2024", 12},
		{"https://everybody.codes/event/2024/quests/01?foo=bar#part1", "2024", 1},
	} {
		got, err := parseQuestURL(tt.url)
		if err != nil || got.event != tt.event || got.number != tt.number {
			t.Errorf("parse %s = %v, %v", tt.url, got, err)
		}
	}
	for _, raw := range []string{
		"", "not a URL", "http://everybody.codes/event/2024/quests/1",
		"https://example.com/event/2024/quests/1",
		"https://everybody.codes.evil.example/event/2024/quests/1",
		"https://user:pass@everybody.codes/event/2024/quests/1",
		"https://everybody.codes/event/../quests/1",
		"https://everybody.codes/event/%2e%2e/quests/1",
		"https://everybody.codes/event/2024/quests/0",
		"https://everybody.codes/event/2024/quests/-1",
		"https://everybody.codes/event/2024/quests/99999999999999999999",
		"https://everybody.codes/event/2024/quests/one",
		"https://everybody.codes/event/2024/quests/1/extra",
	} {
		if _, err := parseQuestURL(raw); err == nil {
			t.Errorf("accepted invalid URL %q", raw)
		}
	}
}

func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown", testURL}, {testURL, "extra"}, {"init", testURL}} {
		if err := run(context.Background(), args, io.Discard); err == nil || err.Error() != usage {
			t.Errorf("run(%v) error = %v, want usage", args, err)
		}
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--dir", "elsewhere", "--help"}, {testURL, "-h"}} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err != nil || strings.TrimSpace(output.String()) != usage {
			t.Errorf("help output = %q, error = %v", output.String(), err)
		}
	}
}
