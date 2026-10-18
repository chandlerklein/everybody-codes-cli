package update

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateHomebrewExecutableDetection(t *testing.T) {
	for _, tt := range []struct {
		path string
		want bool
	}{
		{"custom-prefix/Caskroom/ec/0.1.0/ec", true},
		{"bin/ec", false},
		{"Caskroom/other/0.1.0/ec", false},
		{"Caskroom/ec/0.1.0/other", false},
		{"ec/0.1.0/ec", false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), filepath.FromSlash(tt.path))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("executable"), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := homebrewExecutable(target); got != tt.want {
				t.Fatalf("homebrewExecutable = %v, want %v", got, tt.want)
			}
			if runtime.GOOS == "windows" {
				return // Creating symlinks requires elevated privileges on Windows.
			}
			link := filepath.Join(t.TempDir(), "ec")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if got := homebrewExecutable(link); got != tt.want {
				t.Fatalf("homebrewExecutable through symlink = %v, want %v", got, tt.want)
			}
		})
	}
	if homebrewExecutable(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("missing executable detected as Homebrew")
	}
}

func TestReplaceExecutableThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires elevated privileges")
	}
	f := setupUpdate(t, "linux", []byte("new executable"))
	link := filepath.Join(t.TempDir(), "ec-link")
	if err := os.Symlink(f.target, link); err != nil {
		t.Fatal(err)
	}
	f.u.executable = func() (string, error) { return link, nil }
	if err := f.u.run(t.Context(), "v0.1.0", io.Discard); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was replaced: %v, %v", info, err)
	}
	assertFile(t, f.target, "new executable")
	assertFile(t, link, "new executable")
}

func TestCommitUpdateFailurePreservesExecutable(t *testing.T) {
	f := setupUpdate(t, "linux", []byte("new executable"))
	if err := commitUpdate(filepath.Join(t.TempDir(), "missing"), f.target); err == nil {
		t.Fatal("expected rename failure")
	}
	assertFile(t, f.target, "old executable")
}

func TestReplaceExecutableRejectsDirectory(t *testing.T) {
	if err := replaceExecutable(t.TempDir(), []byte("new executable")); err == nil || !strings.Contains(err.Error(), "not a regular executable") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateReadOnlyInstallationKeepsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions without root privileges")
	}
	f := setupUpdate(t, "linux", []byte("new executable"))
	dir := filepath.Dir(f.target)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := f.u.run(t.Context(), "v0.1.0", io.Discard); err == nil || !strings.Contains(err.Error(), "installation directory") {
		t.Fatalf("error = %v", err)
	}
	assertFile(t, f.target, "old executable")
}

// Exercise replacement of a running process on each native CI platform.
func TestUpdateRunningExecutable(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "ec-test")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.WriteFile(target, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stat an open handle to capture the original Windows file ID before replacement.
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.Stat()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), target, "-test.run=^TestUpdateRunningExecutableHelper$")
	cmd.Env = append(os.Environ(), "EC_UPDATE_TEST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("Updated ec from v0.1.0 to v0.2.0")) {
		t.Fatalf("running update: %v\n%s", err, output)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("executable file was not replaced")
	}
	cmd = exec.CommandContext(t.Context(), target, "-test.run=^TestUpdateRunningExecutableHelper$")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("updated executable does not run: %v\n%s", err, output)
	}
}

func TestUpdateRunningExecutableHelper(t *testing.T) {
	if os.Getenv("EC_UPDATE_TEST_HELPER") != "1" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	f := setupUpdate(t, runtime.GOOS, binary)
	f.u.executable = os.Executable
	if err := f.u.run(t.Context(), "v0.1.0", os.Stdout); err != nil {
		t.Fatal(err)
	}
}
