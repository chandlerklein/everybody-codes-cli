package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chandlerklein/everybody-codes-cli/internal/workspace"
)

// resolveWorkspace selects an explicit destination exactly, or discovers the
// nearest workspace above the current directory. No marker means legacy cwd
// behavior without any inferred puzzle identifiers.
func resolveWorkspace(dir string, explicit bool) (string, workspace.Context, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", workspace.Context{}, fmt.Errorf("get current directory: %w", err)
	}
	if explicit {
		if dir == "" {
			return "", workspace.Context{}, fmt.Errorf("--dir requires a nonempty directory")
		}
		root, err := filepath.Abs(dir)
		if err != nil {
			return "", workspace.Context{}, fmt.Errorf("resolve workspace directory: %w", err)
		}
		found, err := workspace.Validate(root)
		if err != nil {
			return "", workspace.Context{}, err
		}
		if found {
			return root, workspace.Infer(root, cwd), nil
		}
		return root, workspace.Context{}, nil
	}
	root, err := workspace.Find(cwd)
	if err != nil {
		return "", workspace.Context{}, err
	}
	if root == "" {
		return cwd, workspace.Context{}, nil
	}
	return root, workspace.Infer(root, cwd), nil
}
