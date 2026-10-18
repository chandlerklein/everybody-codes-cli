package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestForceReplacesInputLinksWithoutModifyingTargets(t *testing.T) {
	for _, kind := range []string{"external symlink", "internal symlink", "hard link", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			setup(t)
			dir := "2024/quest-01/part-01"
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			const name = "input.txt"
			target := filepath.Join(t.TempDir(), name)
			linkTarget := target
			if kind == "internal symlink" {
				target = filepath.Join(filepath.Dir(dir), "original-"+name)
				linkTarget = "../original-" + name
			}
			if kind != "dangling symlink" {
				writeTestFile(t, target, "preserve original")
			}
			link := os.Symlink
			if kind == "hard link" {
				link = os.Link
			}
			if err := link(linkTarget, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if kind == "dangling symlink" {
					assertMissing(t, target)
				} else {
					assertFile(t, target, "preserve original")
				}
			})
			if err := run(t.Context(), []string{"get", testURL, "--force"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertFile(t, filepath.Join(dir, "input.txt"), testNotes[0].plain)
			assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("part directory entries = %v, error = %v; want only input files", entries, err)
			}
			for _, entry := range entries {
				if !entry.Type().IsRegular() {
					t.Errorf("%s was not replaced by a regular file", entry.Name())
				}
			}
		})
	}
}

func TestAnswerLinksAreAlwaysPreserved(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, kind := range []string{"symlink", "hard link", "dangling symlink", "directory"} {
			t.Run(fmt.Sprintf("%s/force=%t", kind, force), func(t *testing.T) {
				setup(t)
				path := "2024/quest-01/part-01/answer.txt"
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "answer.txt")
				if kind != "dangling symlink" {
					writeTestFile(t, target, "42\n")
				}
				switch kind {
				case "directory":
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
				case "hard link":
					if err := os.Link(target, path); err != nil {
						t.Fatal(err)
					}
				default:
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"get", testURL}
				if force {
					args = append(args, "-f")
				}
				if err := run(t.Context(), args, io.Discard); err != nil {
					t.Fatal(err)
				}
				after, err := os.Lstat(path)
				if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("answer entry changed: %v", err)
				}
				if kind == "dangling symlink" {
					assertMissing(t, target)
				} else {
					assertFile(t, target, "42\n")
				}
			})
		}
	}
}
