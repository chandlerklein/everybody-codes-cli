package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func enterDirectory(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func TestSubmitWorkspaceContext(t *testing.T) {
	for _, tt := range []struct {
		name, dir, year string
		quest, part     int
		args            []string
		stdin           string
	}{
		{"part", "2024/quest-01/part-01", "2024", 1, 1, []string{"1309"}, ""},
		{"solver subdirectory", "2024/quest-12/part-03/rust/src", "2024", 12, 3, []string{"1309"}, ""},
		{"story", "story-1/quest-08/part-02", "story-1", 8, 2, []string{"1309"}, ""},
		{"piped", "2024/quest-01/part-02/python", "2024", 1, 2, nil, "1309\r\n"},
		{"quest directory", "2024/quest-01", "2024", 1, 3, []string{"-p3", "1309"}, ""},
		{"year directory", "2024", "2024", 8, 2, []string{"-q8", "-p2", "1309"}, ""},
		{"workspace root", ".", "2024", 1, 1, []string{"-y2024", "-q1", "-p1", "1309"}, ""},
		{"root current year", ".", fmt.Sprint(time.Now().Year()), 1, 1, []string{"-q1", "-p1", "1309"}, ""},
		{"override part", "2024/quest-01/part-01", "2024", 1, 2, []string{"-p2", "1309"}, ""},
		{"override quest", "2024/quest-01/part-01", "2024", 8, 1, []string{"-q08", "1309"}, ""},
		{"override year", "2024/quest-01/part-01", "story-1", 1, 1, []string{"-ystory-1", "1309"}, ""},
		{"override all", "2024/quest-01/part-01", "2025", 9, 8, []string{"-y2025", "-q9", "-p8", "1309"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := setupSubmission(t, http.StatusOK, `{"correct":false}`)
			writeTestFile(t, ".ec.json", `{"version":1}`)
			enterDirectory(t, filepath.FromSlash(tt.dir))
			var output bytes.Buffer
			if err := executeSubmit(t.Context(), tt.args, strings.NewReader(tt.stdin), &output); err != nil {
				t.Fatal(err)
			}
			wantURL := fmt.Sprintf("%s/event/%s/quest/%d/part/%d/answer", apiBaseURL, tt.year, tt.quest, tt.part)
			if request.calls != 1 || request.url != wantURL || request.answer != "1309" {
				t.Fatalf("submission: calls=%d, URL=%s, answer=%q; want URL=%s", request.calls, request.url, request.answer, wantURL)
			}
			if want := fmt.Sprintf("Year %s, quest %d, part %d\n", tt.year, tt.quest, tt.part); !strings.HasPrefix(output.String(), want) {
				t.Fatalf("target missing from output: %q", output.String())
			}
			assertMissing(t, filepath.Join(tt.year, fmt.Sprintf("quest-%02d", tt.quest)))
		})
	}
}

func TestSubmitMissingContextDoesNotGuess(t *testing.T) {
	for _, tt := range []struct {
		name, dir, marker, want string
		args                    []string
	}{
		{"no marker", "2024/quest-01/part-01", "", "--quest", []string{"1309"}},
		{"root", ".", `{"version":1}`, "--quest", []string{"1309"}},
		{"quest requires part", "2024/quest-01", `{"version":1}`, "--part", []string{"1309"}},
		{"noncanonical quest", "2024/quest-1/part-01", `{"version":1}`, "--quest", []string{"1309"}},
		{"noncanonical part", "2024/quest-01/part-1", `{"version":1}`, "--part", []string{"1309"}},
		{"unknown part", "2024/quest-01/part-04", `{"version":1}`, "--part", []string{"1309"}},
		{"unrelated prefix", "notes/2024/quest-01/part-01", `{"version":1}`, "--quest", []string{"1309"}},
		{"invalid explicit part", "2024/quest-01/part-01", `{"version":1}`, "part number must be", []string{"--part=01", "1309"}},
		{"empty explicit quest", "2024/quest-01/part-01", `{"version":1}`, "quest number must be", []string{"--quest=", "1309"}},
		{"empty explicit year", "2024/quest-01/part-01", `{"version":1}`, "year must be", []string{"--year=", "1309"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := setupSubmission(t, http.StatusOK, `{"correct":false}`)
			if tt.marker != "" {
				writeTestFile(t, ".ec.json", tt.marker)
			}
			enterDirectory(t, filepath.FromSlash(tt.dir))
			var output bytes.Buffer
			err := executeSubmit(t.Context(), tt.args, strings.NewReader(""), &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v; want %q", err, tt.want)
			}
			if request.calls != 0 || len(request.inputs.calls) != 0 || output.Len() != 0 {
				t.Fatalf("invalid context caused network or output: %d, %v, %q", request.calls, request.inputs.calls, output.String())
			}
		})
	}
}

func TestSubmitWithoutMarkerUsesExplicitFlagsOnly(t *testing.T) {
	request := setupSubmission(t, http.StatusOK, `{"correct":true}`)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	enterDirectory(t, "2024/quest-01/part-01")
	if err := executeSubmit(t.Context(), []string{"-y2024", "-q1", "-p1", "1309"}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	if request.calls != 1 {
		t.Fatalf("submission requests = %d", request.calls)
	}
	// Legacy mode uses cwd, even when its name resembles a workspace path.
	assertFile(t, "2024/quest-01/part-02/input.txt", testNotes[1].plain)
	assertMissing(t, ".ec.json")
	assertMissing(t, filepath.Join(root, ".ec.json"))
}

func TestSubmitDownloadsUnlockedInputAtWorkspaceRoot(t *testing.T) {
	request := setupSubmission(t, http.StatusOK, `{"correct":true}`)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	const marker = "{\"version\":1,\"future\":true}\n"
	writeTestFile(t, ".ec.json", marker)
	writeTestFile(t, "2024/quest-01/part-01/input.txt", "keep existing input")
	request.inputs.bodies[keysURL] = jsonString(t, map[string]string{"key2": testKey})
	enterDirectory(t, "2024/quest-01/part-01/python/src")
	if err := executeSubmit(t.Context(), []string{"1309"}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "2024/quest-01/part-02/input.txt"), testNotes[1].plain)
	assertFile(t, filepath.Join(root, "2024/quest-01/part-01/input.txt"), "keep existing input")
	assertFile(t, filepath.Join(root, ".ec.json"), marker)
	assertMissing(t, "2024")
	assertMissing(t, ".ec.json")
	if request.calls != 1 || len(request.inputs.calls) != 3 {
		t.Fatalf("requests: submission=%d, downloads=%v", request.calls, request.inputs.calls)
	}
}

func TestGetCreatesOnlyWorkspaceMarkerAfterSuccess(t *testing.T) {
	for _, mode := range []string{"download", "already complete", "failed download"} {
		t.Run(mode, func(t *testing.T) {
			api := setup(t)
			if mode == "already complete" {
				for part := 1; part <= 3; part++ {
					writeTestFile(t, fmt.Sprintf("2024/quest-01/part-%02d/input.txt", part), "existing")
				}
				if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config/everybody-codes/session")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "failed download" {
				api.statuses[userURL] = http.StatusUnauthorized
			}
			err := run(t.Context(), []string{"get", testURL}, io.Discard)
			if mode == "failed download" {
				if err == nil {
					t.Fatal("failed download succeeded")
				}
				assertMissing(t, ".ec.json")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertFile(t, ".ec.json", "{\n  \"version\": 1\n}\n")
			for part := 1; part <= 3; part++ {
				assertMissing(t, fmt.Sprintf("2024/quest-01/part-%02d/.ec.json", part))
			}
			if mode == "already complete" && len(api.calls) != 0 {
				t.Fatalf("migration requested network: %v", api.calls)
			}
		})
	}
}

func TestGetFromNestedDirectoryReusesWorkspace(t *testing.T) {
	for _, args := range [][]string{{"get"}, {"get", testURL}, {"get", "-q1"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setup(t)
			root, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			const marker = "{ \"version\": 1, \"future\": true }\n"
			writeTestFile(t, ".ec.json", marker)
			enterDirectory(t, "2024/quest-01/part-01/python")
			if err := run(t.Context(), args, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertFile(t, filepath.Join(root, "2024/quest-01/part-02/input.txt"), testNotes[1].plain)
			assertFile(t, filepath.Join(root, ".ec.json"), marker)
			assertMissing(t, "2024")
			assertMissing(t, ".ec.json")
		})
	}
}

func TestExplicitDirectorySelectsExactDestination(t *testing.T) {
	setup(t)
	writeTestFile(t, ".ec.json", "invalid parent config")
	if err := run(t.Context(), []string{"get", testURL, "--dir", "separate"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertFile(t, "separate/.ec.json", "{\n  \"version\": 1\n}\n")
	assertFile(t, "separate/2024/quest-01/part-01/input.txt", testNotes[0].plain)
	assertFile(t, ".ec.json", "invalid parent config")
	assertMissing(t, "2024")
	if err := run(t.Context(), []string{"status", "--dir", "separate"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidMarkerStopsCommandsBeforeNetworkOrWrites(t *testing.T) {
	for _, args := range [][]string{{"get", testURL}, {"get", testURL, "--dir", "."}, {"status"}, {"status", "--dir", "."}, {"submit", "-y2024", "-q1", "-p1", "1309"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			api := setup(t)
			writeTestFile(t, ".ec.json", `{"version":2}`)
			var output bytes.Buffer
			err := run(t.Context(), args, &output)
			if err == nil || !strings.Contains(err.Error(), "unsupported workspace config version") {
				t.Fatalf("error = %v", err)
			}
			if len(api.calls) != 0 || output.Len() != 0 {
				t.Fatalf("invalid config caused requests %v or output %q", api.calls, output.String())
			}
			assertMissing(t, "2024")
			assertFile(t, ".ec.json", `{"version":2}`)
		})
	}
}

func TestStatusWorkspaceAndMissingMarker(t *testing.T) {
	for _, marker := range []bool{false, true} {
		t.Run(fmt.Sprintf("marker=%t", marker), func(t *testing.T) {
			api := setup(t)
			root, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, "2024/quest-01/part-01/input.txt", "input")
			if marker {
				writeTestFile(t, ".ec.json", `{"version":1}`)
			}
			var output bytes.Buffer
			if err := run(t.Context(), []string{"status"}, &output); err != nil {
				t.Fatal(err)
			}
			want := output.String()
			enterDirectory(t, "2024/quest-01/part-01/python")
			output.Reset()
			if err := run(t.Context(), []string{"status"}, &output); err != nil {
				t.Fatal(err)
			}
			if !marker {
				want = "No local quests found.\n"
				assertMissing(t, filepath.Join(root, ".ec.json"))
			}
			if output.String() != want || len(api.calls) != 0 {
				t.Fatalf("status=%q, want=%q; requests=%v", output.String(), want, api.calls)
			}
			assertMissing(t, ".ec.json")
		})
	}
}
