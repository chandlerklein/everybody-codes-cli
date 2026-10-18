package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

func initQuest(ctx context.Context, q quest, baseDir string, force bool, output io.Writer) error {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return fmt.Errorf("open output directory: %w", err)
	}
	defer root.Close()
	dir := filepath.Join(q.year, fmt.Sprintf("quest-%02d", q.number))
	displayDir := filepath.Join(baseDir, dir)
	var pending []int
	for part := 1; part <= 3; part++ {
		partPath := partDir(dir, part)
		if err := root.MkdirAll(partPath, 0o755); err != nil {
			return fmt.Errorf("create part directory: %w", err)
		}
		inputPath := filepath.Join(partPath, "input.txt")
		var info os.FileInfo
		if force {
			// Inspect the entry itself: replacement must never follow a link.
			info, err = root.Lstat(inputPath)
		} else {
			info, err = root.Stat(inputPath)
		}
		switch {
		case errors.Is(err, os.ErrNotExist):
			pending = append(pending, part)
		case err != nil:
			return fmt.Errorf("check part %d notes: %w", part, err)
		case !info.Mode().IsRegular() && !(force && info.Mode()&os.ModeSymlink != 0):
			return fmt.Errorf("part %d input.txt must be a regular file", part)
		case force:
			pending = append(pending, part)
		}
	}
	if len(pending) == 0 {
		_, err := fmt.Fprintf(output, "%s: all notes already exist\n", displayDir)
		return err
	}
	cookie, err := loadSession()
	if err != nil {
		return err
	}
	// Keys need no seed. Fetch them alongside the encoded inputs.
	ctx, cancel := context.WithCancel(ctx)
	keysURL := fmt.Sprintf("%s/event/%s/quest/%d", apiBaseURL, q.year, q.number)
	var keys map[string]json.RawMessage
	var keysErr error
	keysDone := make(chan struct{})
	go func() {
		defer close(keysDone)
		keysErr = getJSON(ctx, keysURL, cookie, &keys)
	}()
	defer func() {
		cancel()
		<-keysDone
	}()

	var user struct {
		Seed *int64 `json:"seed"`
	}
	if err := getJSON(ctx, apiBaseURL+"/user/me", cookie, &user); err != nil {
		return fmt.Errorf("get user seed: %w", err)
	}
	if user.Seed == nil || *user.Seed < 0 {
		return errors.New("user response must contain a nonnegative integer seed")
	}
	inputURL := fmt.Sprintf("https://everybody.codes/assets/%s/%d/input/%d.json", q.year, q.number, *user.Seed)
	var inputs map[string]string
	if err := getJSON(ctx, inputURL, cookie, &inputs); err != nil {
		// Missing assets can return an HTML page with HTTP 200. In that
		// case, use the API's result to identify an unavailable quest.
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			<-keysDone
			if questUnavailable(keysErr) {
				return questDownloadError(q, "get quest keys", keysErr)
			}
		}
		return questDownloadError(q, "get encoded notes", err)
	}
	<-keysDone
	if keysErr != nil {
		return questDownloadError(q, "get quest keys", keysErr)
	}
	for _, part := range pending {
		var key string
		if raw, ok := keys[fmt.Sprintf("key%d", part)]; ok {
			if err := json.Unmarshal(raw, &key); err != nil {
				return fmt.Errorf("key%d must be a string or null", part)
			}
		}
		if key == "" {
			if _, err := fmt.Fprintf(output, "Part %d: locked; key not available yet\n", part); err != nil {
				return err
			}
			continue
		}
		encoded := inputs[strconv.Itoa(part)]
		if encoded == "" {
			return fmt.Errorf("input response is missing encoded notes for part %d", part)
		}
		partPath := partDir(dir, part)
		notes, err := decryptNotes(encoded, key)
		if err != nil {
			return fmt.Errorf("decode part %d notes: %w", part, err)
		}
		write := writeNotes
		if force {
			write = replaceFile
		}
		if err := write(root, filepath.Join(partPath, "input.txt"), notes); err != nil {
			return fmt.Errorf("write part %d notes: %w", part, err)
		}
		if _, err := fmt.Fprintf(output, "Part %d: decoded notes saved\n", part); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output, "Initialized %s\n", displayDir)
	return err
}
