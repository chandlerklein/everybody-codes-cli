package everybodycodes

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

// Rename replaces the destination entry without following symlinks or
// truncating another file linked to the same inode.
func replaceFile(root *os.Root, path string, data []byte) error {
	temp := filepath.Join(filepath.Dir(path), ".ec-"+rand.Text()+".tmp")
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return root.Rename(temp, path)
}

// Exclusive creation also preserves notes written by another ec process.
func writeNotes(root *os.Root, path string, notes []byte) error {
	f, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := f.Write(notes)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		// Do not leave an incomplete input.txt that a later run would skip.
		_ = root.Remove(path)
		return err
	}
	return nil
}
