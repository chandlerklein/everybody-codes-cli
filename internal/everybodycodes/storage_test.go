package everybodycodes

import (
	"os"
	"path/filepath"
	"testing"
)

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
