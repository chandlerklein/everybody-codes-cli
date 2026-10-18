package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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
