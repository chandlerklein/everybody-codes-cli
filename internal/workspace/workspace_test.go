package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeMarker(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindNearestWorkspace(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, `{"version":1}`)
	nested := filepath.Join(root, "nested")
	writeMarker(t, nested, `{"version":1}`)
	start := filepath.Join(nested, "2024", "quest-01", "part-01", "python")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Find(start)
	if err != nil || got != nested {
		t.Fatalf("Find(%q) = %q, %v; want %q", start, got, err, nested)
	}
	writeMarker(t, nested, `{"version":2}`)
	if _, err := Find(start); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("invalid nearest marker was skipped: %v", err)
	}
}

func TestMissingWorkspace(t *testing.T) {
	root := t.TempDir()
	// Make the search independent of markers outside the temporary tree.
	for dir := filepath.Dir(root); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, Filename)); !os.IsNotExist(err) {
			t.Skipf("ancestor contains a marker or cannot be inspected: %s", dir)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	got, err := Find(root)
	if err != nil || got != "" {
		t.Fatalf("missing workspace = %q, %v", got, err)
	}
	if found, err := Validate(filepath.Join(root, "not-created")); found || err != nil {
		t.Fatalf("missing destination = %t, %v", found, err)
	}
	if _, err := os.Stat(filepath.Join(root, Filename)); !os.IsNotExist(err) {
		t.Fatalf("discovery created a marker: %v", err)
	}
}

func TestValidateMarker(t *testing.T) {
	for _, tt := range []struct {
		name, contents, want string
	}{
		{"valid", `{"version":1}`, ""},
		{"preserve extra settings", `{"version":1,"future":true}`, ""},
		{"whitespace", " \n{\"version\":1}\n\t", ""},
		{"missing version", `{}`, "version"},
		{"null object", `null`, "version"},
		{"null version", `{"version":null}`, "version"},
		{"unsupported version", `{"version":2}`, "unsupported"},
		{"old version", `{"version":0}`, "unsupported"},
		{"string version", `{"version":"1"}`, "invalid"},
		{"fractional version", `{"version":1.5}`, "invalid"},
		{"malformed", `{"version":`, "invalid"},
		{"empty", "", "invalid"},
		{"array", `[{"version":1}]`, "invalid"},
		{"trailing object", `{"version":1}{"version":1}`, "exactly one"},
		{"trailing junk", `{"version":1}broken`, "exactly one"},
		{"too large", `{"version":1}` + strings.Repeat(" ", 65536), "exceeds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeMarker(t, root, tt.contents)
			found, err := Validate(root)
			if tt.want == "" {
				if !found || err != nil {
					t.Fatalf("valid marker = %t, %v", found, err)
				}
			} else if found || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("invalid marker = %t, %v; want %q", found, err, tt.want)
			}
		})
	}
}

func TestMarkerMustBeRegularFile(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "dangling symlink", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, Filename)
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink", "dangling symlink":
				target := filepath.Join(t.TempDir(), "config")
				if kind == "symlink" {
					if err := os.WriteFile(target, []byte(`{"version":1}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "unreadable":
				writeMarker(t, root, `{"version":1}`)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
				if _, err := os.ReadFile(path); err == nil {
					t.Skip("current user can read permission-restricted files")
				}
			}
			if found, err := Validate(root); found || err == nil {
				t.Fatalf("accepted %s marker: %t, %v", kind, found, err)
			}
			if err := Create(root); err == nil {
				t.Fatalf("creation accepted existing %s marker", kind)
			}
		})
	}
}

func TestCreatePreservesExistingMarker(t *testing.T) {
	root := t.TempDir()
	if err := Create(root); err != nil {
		t.Fatal(err)
	}
	if found, err := Validate(root); !found || err != nil {
		t.Fatalf("created marker = %t, %v", found, err)
	}
	const contents = "{ \"version\": 1, \"future\": \"keep\" }\n"
	writeMarker(t, root, contents)
	path := filepath.Join(root, Filename)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Create(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != contents || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("existing marker changed: contents=%q, error=%v", data, err)
	}
}

func TestConcurrentWorkspaceInitialization(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		root := t.TempDir()
		const workers = 32
		start := make(chan struct{})
		errors := make(chan error, workers)
		var done sync.WaitGroup
		for i := 0; i < workers; i++ {
			done.Go(func() {
				<-start
				errors <- Create(root)
			})
		}
		close(start)
		done.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatalf("concurrent workspace initialization failed: %v", err)
			}
		}
		if found, err := Validate(root); !found || err != nil {
			t.Fatalf("initialized workspace = %t, %v", found, err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || entries[0].Name() != Filename {
			t.Fatalf("initialization left unexpected files: %v, %v", entries, err)
		}
	}
}

func TestInferWorkspaceContext(t *testing.T) {
	root := t.TempDir()
	for _, tt := range []struct {
		path string
		want Context
	}{
		{".", Context{}},
		{"2024", Context{Year: "2024"}},
		{"2024/quest-01", Context{Year: "2024", Quest: "1"}},
		{"2024/quest-01/part-01", Context{Year: "2024", Quest: "1", Part: "1"}},
		{"2024/quest-12/part-03/rust/src", Context{Year: "2024", Quest: "12", Part: "3"}},
		{"story-1/quest-08/part-02/python", Context{Year: "story-1", Quest: "8", Part: "2"}},
		{"2024/quest-100/part-01", Context{Year: "2024", Quest: "100", Part: "1"}},
		{"2024/quest-1/part-01", Context{Year: "2024"}},
		{"2024/quest-001/part-01", Context{Year: "2024"}},
		{"2024/quest-00/part-01", Context{Year: "2024"}},
		{"2024/quest-99999999999999999999/part-01", Context{Year: "2024"}},
		{"2024/quest-01/part-1", Context{Year: "2024", Quest: "1"}},
		{"2024/quest-01/part-04", Context{Year: "2024", Quest: "1"}},
		{"2024/quest-01/part-00", Context{Year: "2024", Quest: "1"}},
		{"2024/other/quest-01/part-01", Context{Year: "2024"}},
		{"notes/2024/quest-01/part-01", Context{Year: "notes"}},
		{".hidden/quest-01/part-01", Context{}},
		{"../2024/quest-01/part-01", Context{}},
	} {
		t.Run(tt.path, func(t *testing.T) {
			if got := Infer(root, filepath.Join(root, filepath.FromSlash(tt.path))); got != tt.want {
				t.Fatalf("context = %+v, want %+v", got, tt.want)
			}
		})
	}
}
