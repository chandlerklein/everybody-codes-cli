// Package workspace locates and validates Everybody Codes workspaces.
package workspace

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

const Filename = ".ec.json"

const maxConfigBytes = 64 * 1024

// Context contains the puzzle identifiers available from a workspace path.
// Empty fields mean that the corresponding identifier could not be inferred.
type Context struct {
	Year  string
	Quest string
	Part  string
}

// Find walks from start toward the filesystem root. A missing marker returns
// an empty root; an invalid nearest marker is an error, never skipped.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve workspace directory: %w", err)
	}
	for {
		found, err := Validate(dir)
		if err != nil {
			return "", err
		}
		if found {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// Validate checks the marker in exactly dir. A missing directory or marker is
// allowed. Existing markers must be regular files containing version 1 JSON.
func Validate(dir string) (bool, error) {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open workspace directory %s: %w", dir, err)
	}
	defer root.Close()
	return validate(root, dir)
}

func validate(root *os.Root, dir string) (bool, error) {
	path := filepath.Join(dir, Filename)
	info, err := root.Lstat(Filename)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect workspace config %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("workspace config %s must be a regular file, not a symlink or directory", path)
	}
	f, err := root.Open(Filename)
	if err != nil {
		return false, fmt.Errorf("read workspace config %s: %w", path, err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect open workspace config %s: %w", path, err)
	}
	current, err := root.Lstat(Filename)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, opened) || !os.SameFile(opened, current) {
		return false, fmt.Errorf("workspace config %s changed while being opened; try again", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return false, fmt.Errorf("read workspace config %s: %w", path, err)
	}
	if len(data) > maxConfigBytes {
		return false, fmt.Errorf("workspace config %s exceeds %d bytes", path, maxConfigBytes)
	}
	var config struct {
		Version *int `json:"version"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&config); err != nil {
		return false, fmt.Errorf("invalid workspace config %s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("invalid workspace config %s: expected exactly one JSON object", path)
	}
	if config.Version == nil {
		return false, fmt.Errorf("workspace config %s requires a version field (expected 1)", path)
	}
	if *config.Version != 1 {
		return false, fmt.Errorf("unsupported workspace config version %d in %s (expected 1)", *config.Version, path)
	}
	return true, nil
}

// Create initializes a marker after a successful download. Publish a complete
// file atomically without replacing a marker created by another ec process.
func Create(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open workspace directory %s: %w", dir, err)
	}
	defer root.Close()
	if found, err := validate(root, dir); err != nil || found {
		return err
	}
	temp := ".ec-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create workspace config %s: %w", filepath.Join(dir, Filename), err)
	}
	defer root.Remove(temp)
	_, writeErr := io.WriteString(f, "{\n  \"version\": 1\n}\n")
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write workspace config %s: %w", filepath.Join(dir, Filename), err)
	}
	// A hard link publishes the fully written inode and fails if Filename
	// already exists. Rename would overwrite another process's marker.
	if err := root.Link(temp, Filename); errors.Is(err, os.ErrExist) {
		_, err = validate(root, dir)
		return err
	} else if err != nil {
		return fmt.Errorf("publish workspace config %s: %w", filepath.Join(dir, Filename), err)
	}
	return nil
}

// Infer reads only the generated path prefix <year>/quest-NN/part-NN.
// Other directory names never supply a quest or part; solver subdirectories
// beneath a recognized part retain the complete context.
func Infer(root, dir string) Context {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Context{}
	}
	components := strings.Split(rel, string(filepath.Separator))
	if _, err := everybodycodes.ParseQuestFlags(components[0], "1"); err != nil {
		return Context{}
	}
	context := Context{Year: components[0]}
	if len(components) < 2 {
		return context
	}
	context.Quest = canonicalNumber(components[1], "quest")
	if context.Quest == "" || len(components) < 3 {
		return context
	}
	part := canonicalNumber(components[2], "part")
	if part == "1" || part == "2" || part == "3" {
		context.Part = part
	}
	return context
}

func canonicalNumber(component, prefix string) string {
	value, err := strconv.Atoi(strings.TrimPrefix(component, prefix+"-"))
	if err != nil || value < 1 || component != fmt.Sprintf("%s-%02d", prefix, value) {
		return ""
	}
	return strconv.Itoa(value)
}
