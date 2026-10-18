//go:build !windows

package cli

import "os"

func commitUpdate(staged, target string) error {
	return os.Rename(staged, target)
}
