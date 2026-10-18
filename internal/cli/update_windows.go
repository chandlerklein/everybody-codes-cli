package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// Windows can rename a running executable but cannot replace it directly.
// Keep its previous image until the next update, when it is no longer running.
func commitUpdate(staged, target string) error {
	backup := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".old")
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous update backup: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		if rollbackErr := os.Rename(backup, target); rollbackErr != nil {
			return fmt.Errorf("%w; rollback failed: %v; restore ec manually from %s", err, rollbackErr, backup)
		}
		return err
	}
	// This normally fails while the old process is running; the next update retries.
	_ = os.Remove(backup)
	return nil
}
