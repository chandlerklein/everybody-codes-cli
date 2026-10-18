package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveReplacesSessionLinksWithoutChangingTarget(t *testing.T) {
	for _, kind := range []string{"external symlink", "internal symlink", "hard link"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, ".config", "everybody-codes")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "original")
			if kind == "internal symlink" {
				target = filepath.Join(dir, "original")
			}
			writeTestFile(t, target, "preserve original")
			path := filepath.Join(dir, "session")
			var err error
			if kind == "hard link" {
				err = os.Link(target, path)
			} else if kind == "internal symlink" {
				err = os.Symlink("original", path)
			} else {
				err = os.Symlink(target, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := Save("replacement-cookie"); err != nil {
				t.Fatal(err)
			}
			assertFile(t, target, "preserve original")
			assertFile(t, path, "replacement-cookie\n")
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatalf("session is not a regular file with mode 0600: %v, %v", info, err)
			}
		})
	}
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
