//go:build !windows

package update

import "os"

func commitUpdate(staged, target string) error {
	return os.Rename(staged, target)
}
