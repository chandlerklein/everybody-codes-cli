package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStatusWithDirectory(t *testing.T) {
	base := t.TempDir()
	statusTestFile(t, base, "2024/quest-01/part-02/input/input.txt", "decoded")
	for _, args := range [][]string{
		{"status", "--dir", base},
		{"--dir", base, "status"},
	} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err != nil {
			t.Fatal(err)
		}
		if got, want := output.String(), "2024\n  quest-01\n    part-01: locked\n    part-02: ready\n    part-03: locked\n"; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestStatusGroupsEventsAndOrdersQuestsNumerically(t *testing.T) {
	base := t.TempDir()
	for path, content := range map[string]string{
		"2025/quest-01/part-01/input/input.txt":         "",
		"2024/quest-10/part-01/input/input-encoded.txt": "encoded",
		"2024/quest-2/part-02/input/input.txt":          "decoded",
		"2024/quest-01/part-01/input/input-encoded.txt": "encoded",
		"story-1/quest-01/part-03/input/input.txt":      "decoded",
		"2024/other/part-01/input/input.txt":            "ignore",
		"2024/quest-00/part-01/input/input.txt":         "ignore",
		"2024/quest-03/other/input/input.txt":           "ignore",
		"2024/quest-04/part-00/input/input.txt":         "ignore",
		"2024/quest-05":                                 "not a directory",
		"README.md":                                     "ignore",
	} {
		statusTestFile(t, base, path, content)
	}
	if err := os.MkdirAll(filepath.Join(base, "2023"), 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runStatus(base, &output); err != nil {
		t.Fatal(err)
	}
	want := `2024
  quest-01
    part-01: locked
    part-02: locked
    part-03: locked
  quest-2
    part-01: locked
    part-02: ready
    part-03: locked
  quest-03
    part-01: locked
    part-02: locked
    part-03: locked
  quest-04
    part-01: locked
    part-02: locked
    part-03: locked
  quest-10
    part-01: locked
    part-02: locked
    part-03: locked
2025
  quest-01
    part-01: ready
    part-02: locked
    part-03: locked
story-1
  quest-01
    part-01: locked
    part-02: locked
    part-03: ready
`
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestStatusEmptyDirectory(t *testing.T) {
	var output bytes.Buffer
	if err := runStatus(t.TempDir(), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "No local quests found.\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStatusReportsEachPartIndependently(t *testing.T) {
	for _, test := range []struct {
		name  string
		files []string
		want  string
	}{
		{
			name: "first part ready and later parts locked",
			files: []string{
				"part-01/input/input.txt",
				"part-02/input/input-encoded.txt",
				"part-03/input/input-encoded.txt",
			},
			want: "2024\n  quest-01\n    part-01: ready\n    part-02: locked\n    part-03: locked\n",
		},
		{
			name: "all parts ready",
			files: []string{
				"part-01/input/input.txt",
				"part-02/input/input.txt",
				"part-03/input/input.txt",
			},
			want: "2024\n  quest-01\n    part-01: ready\n    part-02: ready\n    part-03: ready\n",
		},
		{
			name: "all parts have only encoded inputs",
			files: []string{
				"part-01/input/input-encoded.txt",
				"part-02/input/input-encoded.txt",
				"part-03/input/input-encoded.txt",
			},
			want: "2024\n  quest-01\n    part-01: locked\n    part-02: locked\n    part-03: locked\n",
		},
		{
			name: "all part directories missing",
			want: "2024\n  quest-01\n    part-01: locked\n    part-02: locked\n    part-03: locked\n",
		},
		{
			name:  "missing first part and third part directories",
			files: []string{"part-02/input/input.txt"},
			want:  "2024\n  quest-01\n    part-01: locked\n    part-02: ready\n    part-03: locked\n",
		},
		{
			name: "ignore noncanonical parts and extra parts",
			files: []string{
				"part-1/input/input.txt",
				"part-2/input/input-encoded.txt",
				"part-3/input/input.txt",
				"part-04/input/input.txt",
				"part-001/input/input.txt",
				"part-002/input/input.txt",
				"part-003/input/input.txt",
			},
			want: "2024\n  quest-01\n    part-01: locked\n    part-02: locked\n    part-03: locked\n",
		},
		{
			name: "canonical parts determine readiness alongside noncanonical parts",
			files: []string{
				"part-01/input/input-encoded.txt",
				"part-02/input/input.txt",
				"part-03/input/input-encoded.txt",
				"part-1/input/input.txt",
				"part-2/input/input-encoded.txt",
				"part-003/input/input.txt",
			},
			want: "2024\n  quest-01\n    part-01: locked\n    part-02: ready\n    part-03: locked\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			questPath := "2024/quest-01"
			if err := os.MkdirAll(filepath.Join(base, questPath), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, path := range test.files {
				statusTestFile(t, base, filepath.Join(questPath, path), "notes")
			}
			var output bytes.Buffer
			if err := runStatus(base, &output); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != test.want {
				t.Fatalf("output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStatusDirectoryNamedInputIsLocked(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "2024/quest-01/part-01/input/input.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runStatus(base, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "2024\n  quest-01\n    part-01: locked\n    part-02: locked\n    part-03: locked\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStatusAllowsInternalLinks(t *testing.T) {
	base := t.TempDir()
	statusTestFile(t, base, "2024/quest-01/part-01/input/notes.txt", "decoded")
	if err := os.Symlink("notes.txt", filepath.Join(base, "2024/quest-01/part-01/input/input.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("quest-01", filepath.Join(base, "2024/quest-02")); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runStatus(base, &output); err != nil {
		t.Fatal(err)
	}
	want := `2024
  quest-01
    part-01: ready
    part-02: locked
    part-03: locked
  quest-02
    part-01: ready
    part-02: locked
    part-03: locked
`
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStatusRejectsLinksOutsideBaseDirectory(t *testing.T) {
	for _, path := range []string{
		"2024",
		"2024/quest-01",
		"2024/quest-01/part-01",
		"2024/quest-01/part-01/input",
		"2024/quest-01/part-01/input/input.txt",
	} {
		t.Run(path, func(t *testing.T) {
			base := t.TempDir()
			outside := t.TempDir()
			statusTestFile(t, outside, "input.txt", "decoded")
			linkPath := filepath.Join(base, path)
			if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
				t.Fatal(err)
			}
			target := outside
			if strings.HasSuffix(path, "input.txt") {
				target = filepath.Join(outside, "input.txt")
			}
			if err := os.Symlink(target, linkPath); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := runStatus(base, &output); err == nil {
				t.Fatal("accepted a link escaping the status directory")
			}
			if output.Len() != 0 {
				t.Fatalf("printed partial status: %q", output.String())
			}
		})
	}
}

func TestStatusMissingDirectory(t *testing.T) {
	var output bytes.Buffer
	err := runStatus(filepath.Join(t.TempDir(), "missing"), &output)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want missing directory", err)
	}
}

type statusErrorWriter struct {
	err error
}

func (w statusErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStatusPropagatesOutputError(t *testing.T) {
	base := t.TempDir()
	statusTestFile(t, base, "2024/quest-01/part-01/input/input.txt", "decoded")
	want := errors.New("output unavailable")
	if err := runStatus(base, statusErrorWriter{want}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func statusTestFile(t *testing.T, base, path, contents string) {
	t.Helper()
	fullPath := filepath.Join(base, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
