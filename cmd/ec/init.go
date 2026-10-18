package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	apiBaseURL           = "https://api.everybody.codes"
	maxJSONResponseBytes = 16 << 20
)

var apiClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var questPath = regexp.MustCompile(`^/event/([A-Za-z0-9][A-Za-z0-9_-]*)/quests/([0-9]+)/?$`)

type quest struct {
	event  string
	number int
}

func parseQuestURL(raw string) (quest, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "everybody.codes" || u.User != nil {
		return quest{}, errors.New("expected a quest URL: https://everybody.codes/event/<EVENT>/quests/<QUEST>")
	}
	match := questPath.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return quest{}, errors.New("expected a quest URL: https://everybody.codes/event/<EVENT>/quests/<QUEST>")
	}
	number, err := strconv.Atoi(match[2])
	if err != nil || number < 1 {
		return quest{}, errors.New("quest number must be a positive integer")
	}
	return quest{event: match[1], number: number}, nil
}

func loadSession() (*http.Cookie, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	path := filepath.Join(home, ".config", "everybody-codes", "session")
	value, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read session file %s: %w", path, err)
	}
	session := strings.TrimSpace(string(value))
	source := fmt.Sprintf("session file %s", path)
	if session == "" {
		session = strings.TrimSpace(os.Getenv("EVERYBODY_CODES_SESSION_COOKIE"))
		source = "EVERYBODY_CODES_SESSION_COOKIE"
		if session == "" {
			return nil, fmt.Errorf("session file %s is missing or empty; add your cookie there or set EVERYBODY_CODES_SESSION_COOKIE", path)
		}
	}
	cookie := &http.Cookie{Name: "everybody-codes", Value: session}
	if cookie.Valid() != nil {
		return nil, fmt.Errorf("%s must contain a valid raw cookie value", source)
	}
	return cookie, nil
}

func initQuest(ctx context.Context, q quest, baseDir string, output io.Writer) error {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return fmt.Errorf("open output directory: %w", err)
	}
	defer root.Close()
	dir := filepath.Join(q.event, fmt.Sprintf("quest-%02d", q.number))
	displayDir := filepath.Join(baseDir, dir)
	var pending []int
	for part := 1; part <= 3; part++ {
		inputDir := partInputDir(dir, part)
		if err := root.MkdirAll(inputDir, 0o755); err != nil {
			return fmt.Errorf("create input directory: %w", err)
		}
		info, err := root.Stat(filepath.Join(inputDir, "input.txt"))
		switch {
		case errors.Is(err, os.ErrNotExist):
			pending = append(pending, part)
		case err != nil:
			return fmt.Errorf("check part %d notes: %w", part, err)
		case !info.Mode().IsRegular():
			return fmt.Errorf("part %d input.txt must be a regular file", part)
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
	// Keys need no seed, so fetch them alongside the seed and encoded inputs.
	ctx, cancel := context.WithCancel(ctx)
	keysURL := fmt.Sprintf("%s/event/%s/quest/%d", apiBaseURL, q.event, q.number)
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
	inputURL := fmt.Sprintf("https://everybody.codes/assets/%s/%d/input/%d.json", q.event, q.number, *user.Seed)
	var inputs map[string]string
	if err := getJSON(ctx, inputURL, cookie, &inputs); err != nil {
		return fmt.Errorf("get encoded notes: %w", err)
	}
	for _, part := range pending {
		encoded, ok := inputs[strconv.Itoa(part)]
		if !ok || encoded == "" {
			return fmt.Errorf("input response is missing encoded notes for part %d", part)
		}
		path := filepath.Join(partInputDir(dir, part), "input-encoded.txt")
		if err := writeEncoded(root, path, []byte(encoded)); err != nil {
			return fmt.Errorf("write encoded notes for part %d: %w", part, err)
		}
	}
	<-keysDone
	if keysErr != nil {
		return fmt.Errorf("get quest keys: %w", keysErr)
	}
	for _, part := range pending {
		var key string
		if raw, ok := keys[fmt.Sprintf("key%d", part)]; ok {
			if err := json.Unmarshal(raw, &key); err != nil {
				return fmt.Errorf("key%d must be a string or null", part)
			}
		}
		if key == "" {
			if _, err := fmt.Fprintf(output, "Part %d: encoded notes saved; key not available yet\n", part); err != nil {
				return err
			}
			continue
		}
		inputDir := partInputDir(dir, part)
		notes, err := decryptNotes(inputs[strconv.Itoa(part)], key)
		if err != nil {
			return fmt.Errorf("decode part %d notes: %w", part, err)
		}
		if err := writeNotes(root, filepath.Join(inputDir, "input.txt"), notes); err != nil {
			return fmt.Errorf("write part %d notes: %w", part, err)
		}
		if _, err := fmt.Fprintf(output, "Part %d: decoded notes saved\n", part); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output, "Initialized %s\n", displayDir)
	return err
}

func partInputDir(dir string, part int) string {
	return filepath.Join(dir, fmt.Sprintf("part-%02d", part), "input")
}

func getJSON(ctx context.Context, endpoint string, cookie *http.Cookie, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.AddCookie(cookie)
	resp, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %s", endpoint, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read JSON from %s: %w", endpoint, err)
	}
	if len(body) > maxJSONResponseBytes {
		return fmt.Errorf("JSON response from %s exceeds %d bytes", endpoint, maxJSONResponseBytes)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("invalid JSON from %s: %w", endpoint, err)
	}
	return nil
}

// Rename replaces the destination entry without following symlinks or
// truncating another file linked to the same inode.
func writeEncoded(root *os.Root, path string, encoded []byte) error {
	temp := filepath.Join(filepath.Dir(path), ".ec-"+rand.Text()+".tmp")
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, writeErr := f.Write(encoded)
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
