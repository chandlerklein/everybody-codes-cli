package update

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceExecutable(target string, binary []byte) error {
	target, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular executable file", target)
	}
	// Stage beside the executable so the final rename stays on the same filesystem.
	f, err := os.CreateTemp(filepath.Dir(target), ".ec-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to ec's installation directory; check its permissions: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(binary); err != nil {
		return err
	}
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return commitUpdate(f.Name(), target)
}
