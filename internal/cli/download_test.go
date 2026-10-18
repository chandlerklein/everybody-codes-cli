package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestInitQuest(t *testing.T) {
	api := setup(t)
	// Existing directories must not prevent initialization.
	if err := os.MkdirAll("2024/quest-01/part-02", 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"get", testURL}, &output); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
		assertMissing(t, filepath.Join(dir, "input"))
		assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
		assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
		assertMissing(t, filepath.Join(dir, "answer.txt"))
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
	// A complete quest needs no session and does not create answer files.
	answerPath := "2024/quest-01/part-02/answer.txt"
	if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"get", testURL}, &output); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 3 {
		t.Error("complete quest made additional HTTP requests")
	}
	assertMissing(t, answerPath)
}

func TestInitQuestForceRefreshesCompletedInputs(t *testing.T) {
	for _, args := range [][]string{
		{"get", "--force", testURL},
		{"get", testURL, "-f"},
		{"get", "--force=true", testURL},
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			api := setup(t)
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
				writeTestFile(t, filepath.Join(dir, "input.txt"), "old notes")
			}
			if err := run(t.Context(), args, io.Discard); err != nil {
				t.Fatal(err)
			}
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
				assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
				assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
				assertMissing(t, filepath.Join(dir, "answer.txt"))
			}
			wantCalls := []string{userURL, inputsURL, keysURL}
			slices.Sort(api.calls)
			slices.Sort(wantCalls)
			if !slices.Equal(api.calls, wantCalls) {
				t.Errorf("requests = %v, want %v", api.calls, wantCalls)
			}
		})
	}
}

func TestInitQuestPreservesCompletedFilesByDefault(t *testing.T) {
	for _, flag := range []string{"", "--force=false", "-f=false"} {
		t.Run("flag="+flag, func(t *testing.T) {
			api := setup(t)
			before := map[string]os.FileInfo{}
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
				for _, file := range []string{"input.txt", "answer.txt"} {
					path := filepath.Join(dir, file)
					writeTestFile(t, path, "existing "+file)
					oldTime := time.Unix(1000000000, 0)
					if err := os.Chtimes(path, oldTime, oldTime); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					before[path] = info
				}
			}
			if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")); err != nil {
				t.Fatal(err)
			}
			args := []string{"get", testURL}
			if flag != "" {
				args = append(args, flag)
			}
			if err := run(t.Context(), args, io.Discard); err != nil {
				t.Fatal(err)
			}
			if len(api.calls) != 0 {
				t.Errorf("complete quest made HTTP requests: %v", api.calls)
			}
			for path, original := range before {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(original, info) || !original.ModTime().Equal(info.ModTime()) || original.Mode() != info.Mode() {
					t.Errorf("existing file changed: %s", path)
				}
				file := filepath.Base(path)
				assertFile(t, path, "existing "+file)
			}
		})
	}
}

func TestInitQuestPreservesExistingAnswers(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			setup(t)
			for part := 1; part <= 3; part++ {
				writeTestFile(t, fmt.Sprintf("2024/quest-01/part-%02d/answer.txt", part), fmt.Sprintf("answer %d\n", part))
			}
			args := []string{"get", testURL}
			if force {
				args = append(args, "--force")
			}
			if err := run(t.Context(), args, io.Discard); err != nil {
				t.Fatal(err)
			}
			for part := 1; part <= 3; part++ {
				assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/answer.txt", part), fmt.Sprintf("answer %d\n", part))
			}
		})
	}
}

func TestInitQuestForcePreservesNotesWithoutKeys(t *testing.T) {
	api := setup(t)
	api.bodies[keysURL] = jsonString(t, map[string]any{"key1": testKey, "key2": nil})
	for part := 1; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
		writeTestFile(t, filepath.Join(dir, "input.txt"), "old decoded notes")
	}
	var output bytes.Buffer
	if err := run(t.Context(), []string{"get", "--force", testURL}, &output); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
		assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
		want := "old decoded notes"
		if part == 1 {
			want = testNotes[0].plain
		} else if !strings.Contains(output.String(), fmt.Sprintf("Part %d: locked; key not available yet", part)) {
			t.Errorf("missing unavailable-key message for part %d: %s", part, output.String())
		}
		assertFile(t, filepath.Join(dir, "input.txt"), want)
	}
}

func TestInitQuestForceHTTPFailuresPreserveCompletedInputs(t *testing.T) {
	for _, endpoint := range []string{userURL, inputsURL, keysURL} {
		t.Run(endpoint, func(t *testing.T) {
			api := setup(t)
			api.statuses[endpoint] = http.StatusServiceUnavailable
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
				writeTestFile(t, filepath.Join(dir, "input.txt"), "completed input")
				writeTestFile(t, filepath.Join(dir, "answer.txt"), "saved answer")
			}
			err := run(t.Context(), []string{"get", testURL, "--force"}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
				t.Fatalf("error = %v, want HTTP 503", err)
			}
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
				assertFile(t, filepath.Join(dir, "input.txt"), "completed input")
				assertFile(t, filepath.Join(dir, "answer.txt"), "saved answer")
			}
		})
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
	if err := run(ctx, []string{"get", testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input.txt", part), testNotes[part-1].plain)
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
			err := run(ctx, []string{"get", testURL}, io.Discard)
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
	if err := run(context.Background(), []string{"get", testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for part := 2; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("locked part directory entries = %v, error = %v; want empty", entries, err)
		}
	}
	writeTestFile(t, "2024/quest-01/part-01/input.txt", "keep my existing notes")
	// Completed parts are skipped even if their remote input and key are unusable.
	api.bodies[inputsURL] = jsonString(t, map[string]string{"2": testNotes[1].encoded, "3": testNotes[2].encoded})
	api.bodies[keysURL] = jsonString(t, map[string]any{"key1": 42, "key2": testKey, "key3": testKey})
	api.calls = nil
	if err := run(context.Background(), []string{"get", testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{userURL, inputsURL, keysURL}
	slices.Sort(api.calls)
	slices.Sort(wantCalls)
	if !slices.Equal(api.calls, wantCalls) {
		t.Errorf("requests = %v, want %v", api.calls, wantCalls)
	}
	assertFile(t, "2024/quest-01/part-01/input.txt", "keep my existing notes")
	assertMissing(t, "2024/quest-01/part-01/input-encoded.txt")
	for part := 2; part <= 3; part++ {
		assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input.txt", part), testNotes[part-1].plain)
	}
}

func TestInitQuestWritesOnlyUnlockedInputs(t *testing.T) {
	for _, tt := range []struct {
		name, keys, inputs string
		firstReady         bool
	}{
		{"null and missing keys", jsonString(t, map[string]any{"key1": testKey, "key2": nil}), jsonString(t, map[string]string{"1": testNotes[0].encoded}), true},
		{"empty keys and malformed locked input", jsonString(t, map[string]string{"key1": testKey, "key2": "", "key3": ""}), jsonString(t, map[string]string{"1": testNotes[0].encoded, "2": "invalid ciphertext"}), true},
		{"all locked", `{}`, `{}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			api.bodies[keysURL] = tt.keys
			api.bodies[inputsURL] = tt.inputs
			var output bytes.Buffer
			if err := run(t.Context(), []string{"get", testURL}, &output); err != nil {
				t.Fatal(err)
			}
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("2024/quest-01/part-%02d", part)
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				if part == 1 && tt.firstReady {
					if len(entries) != 1 || entries[0].Name() != "input.txt" {
						t.Fatalf("unlocked part entries = %v; want only input.txt", entries)
					}
					assertFile(t, filepath.Join(dir, "input.txt"), testNotes[0].plain)
				} else {
					if len(entries) != 0 {
						t.Fatalf("locked part entries = %v; want empty", entries)
					}
					if !strings.Contains(output.String(), fmt.Sprintf("Part %d: locked; key not available yet", part)) {
						t.Fatalf("missing locked message for part %d: %s", part, output.String())
					}
				}
			}
			output.Reset()
			if err := run(t.Context(), []string{"status"}, &output); err != nil {
				t.Fatal(err)
			}
			firstState := "locked"
			if tt.firstReady {
				firstState = "ready"
			}
			want := fmt.Sprintf("2024\n  quest-01\n    part-01: %s\n    part-02: locked\n    part-03: locked\n", firstState)
			if output.String() != want {
				t.Fatalf("status = %q, want %q", output.String(), want)
			}
		})
	}
}

func TestInitQuestIgnoresLegacyInputs(t *testing.T) {
	setup(t)
	legacy := map[string]string{
		"2024/quest-01/part-01/input/input-encoded.txt": testNotes[1].encoded,
		"2024/quest-01/part-01/input/input.txt":         "legacy notes",
		"2024/quest-01/part-02/input-encoded.txt":       "invalid legacy ciphertext",
	}
	for path, content := range legacy {
		writeTestFile(t, path, content)
	}
	if err := os.MkdirAll("2024/quest-01/part-03/input-encoded.txt", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"get", testURL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for path, content := range legacy {
		assertFile(t, path, content)
	}
	for part := 1; part <= 3; part++ {
		assertFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input.txt", part), testNotes[part-1].plain)
	}
}

func TestInitQuestTwoDigitNumber(t *testing.T) {
	api := setup(t)
	inputEndpoint := "https://everybody.codes/assets/2024/12/input/8.json"
	keyEndpoint := apiBaseURL + "/event/2024/quest/12"
	api.bodies[inputEndpoint] = api.bodies[inputsURL]
	api.bodies[keyEndpoint] = api.bodies[keysURL]
	delete(api.bodies, inputsURL)
	delete(api.bodies, keysURL)
	if err := run(context.Background(), []string{"get", "https://everybody.codes/event/2024/quests/12"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for part := 1; part <= 3; part++ {
		dir := fmt.Sprintf("2024/quest-12/part-%02d", part)
		assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
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
			if err := run(context.Background(), append([]string{"get"}, tt.args...), io.Discard); err != nil {
				t.Fatal(err)
			}
			for part := 1; part <= 3; part++ {
				dir := filepath.Join(tt.dir, fmt.Sprintf("2024/quest-01/part-%02d", part))
				assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
				assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
			}
			assertMissing(t, "2024")
			if got, err := os.Getwd(); err != nil || got != cwd {
				t.Fatalf("working directory changed: got %q (%v), want %q", got, err, cwd)
			}
			path := filepath.Join(tt.dir, "2024/quest-01/part-01/input.txt")
			writeTestFile(t, path, "preserve these inputs")
			api.calls = nil
			if err := run(context.Background(), append([]string{"get"}, tt.args...), io.Discard); err != nil {
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
		{"missing value", "flag needs an argument", []string{testURL, "--dir"}},
		{"empty value", "requires a nonempty directory", []string{"--dir", "", testURL}},
		{"empty equals value", "requires a nonempty directory", []string{testURL, "--dir="}},
		{"unknown option", "unknown flag", []string{"--directory", "elsewhere", testURL}},
		{"missing URL", "--quest is required when no quest URL is supplied", []string{"--dir", "elsewhere"}},
		{"flag after end of options", "accepts 1 arg(s)", []string{"--", testURL, "--dir", "elsewhere"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := setup(t)
			err := run(context.Background(), append([]string{"get"}, tt.args...), io.Discard)
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
		{"year is a file", "2024", "create part directory", false},
		{"notes is a directory", "2024/quest-01/part-01/input.txt", "must be a regular file", true},
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
			err := run(context.Background(), []string{"get", testURL}, io.Discard)
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
		{"malformed inputs for valid quest", inputsURL, "<html>", "invalid JSON", 0},
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
			err := run(context.Background(), []string{"get", testURL}, &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error()+output.String(), testSession) {
				t.Error("session cookie was exposed")
			}
			assertMissing(t, "2024/quest-01/part-01/input.txt")
		})
	}
}

func TestInitQuestMissingYearOrQuest(t *testing.T) {
	for _, q := range []quest{{year: "2099", number: 1}, {year: "2024", number: 999}} {
		for _, statusCode := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
			for _, failure := range []string{"inputs", "keys", "HTML inputs"} {
				for _, useURL := range []bool{false, true} {
					name := fmt.Sprintf("%s/%d/%d/%s/url=%t", q.year, q.number, statusCode, failure, useURL)
					t.Run(name, func(t *testing.T) {
						api := setup(t)
						keysEndpoint := fmt.Sprintf("%s/event/%s/quest/%d", apiBaseURL, q.year, q.number)
						inputsEndpoint := fmt.Sprintf("https://everybody.codes/assets/%s/%d/input/8.json", q.year, q.number)
						api.bodies[keysEndpoint] = api.bodies[keysURL]
						api.bodies[inputsEndpoint] = api.bodies[inputsURL]
						if failure == "inputs" {
							api.statuses[inputsEndpoint] = statusCode
						} else {
							api.statuses[keysEndpoint] = statusCode
						}
						if failure == "HTML inputs" {
							api.bodies[inputsEndpoint] = "<!doctype html><html>Not found</html>"
						}
						args := []string{"get", "-y", q.year, "-q", fmt.Sprint(q.number)}
						if useURL {
							args = []string{"get", fmt.Sprintf("https://everybody.codes/event/%s/quests/%d", q.year, q.number)}
						}
						err := run(t.Context(), args, io.Discard)
						want := fmt.Sprintf("year %s or quest %d does not exist or is not available yet", q.year, q.number)
						if err == nil || !strings.Contains(err.Error(), want) {
							t.Fatalf("error = %v, want %q", err, want)
						}
						var status *httpStatusError
						if !errors.As(err, &status) || status.statusCode != statusCode {
							t.Fatalf("error did not preserve HTTP status %d: %v", statusCode, err)
						}
						for part := 1; part <= 3; part++ {
							assertMissing(t, fmt.Sprintf("%s/quest-%02d/part-%02d/input.txt", q.year, q.number, part))
						}
					})
				}
			}
		}
	}
}

func TestInitQuestDoesNotReportOtherFailuresAsMissing(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			api := setup(t)
			api.statuses[keysURL] = status
			err := run(t.Context(), []string{"get", "-y", "2024", "-q", "1"}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("error = %v, want HTTP %d", err, status)
			}
			if strings.Contains(err.Error(), "does not exist") {
				t.Fatalf("misreported HTTP %d as a missing quest: %v", status, err)
			}
		})
	}
}
