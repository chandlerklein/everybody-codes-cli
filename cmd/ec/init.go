package main

import (
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/hex"
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
var yearID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
var decimalQuestNumber = regexp.MustCompile(`^[0-9]+$`)

type quest struct {
	year   string
	number int
}

func parseQuestFlags(year, rawNumber string) (quest, error) {
	if !yearID.MatchString(year) {
		return quest{}, errors.New("year must be a nonempty identifier containing only letters, digits, hyphens, or underscores, starting with a letter or digit")
	}
	number, err := strconv.Atoi(rawNumber)
	if err != nil || number < 1 || !decimalQuestNumber.MatchString(rawNumber) {
		return quest{}, errors.New("quest number must be a positive decimal integer (for example, 1 or 01)")
	}
	return quest{year: year, number: number}, nil
}

func parseQuestURL(raw string) (quest, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "everybody.codes" || u.User != nil {
		return quest{}, errors.New("expected a quest URL: https://everybody.codes/event/<YEAR>/quests/<QUEST>")
	}
	match := questPath.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return quest{}, errors.New("expected a quest URL: https://everybody.codes/event/<YEAR>/quests/<QUEST>")
	}
	number, err := strconv.Atoi(match[2])
	if err != nil || number < 1 {
		return quest{}, errors.New("quest number must be a positive integer")
	}
	return quest{year: match[1], number: number}, nil
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
		inputDir := partInputDir(dir, part)
		if err := root.MkdirAll(inputDir, 0o755); err != nil {
			return fmt.Errorf("create input directory: %w", err)
		}
		answerPath := filepath.Join(filepath.Dir(inputDir), "answer.txt")
		if err := createAnswer(root, answerPath); err != nil {
			return fmt.Errorf("create part %d answer: %w", part, err)
		}
		inputPath := filepath.Join(inputDir, "input.txt")
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
	inputs := make(map[string]string, len(pending))
	var missing []int
	for _, part := range pending {
		if force {
			missing = append(missing, part)
			continue
		}
		path := filepath.Join(partInputDir(dir, part), "input-encoded.txt")
		encoded, err := readEncoded(root, path)
		if err != nil {
			return fmt.Errorf("read encoded notes for part %d: %w", part, err)
		}
		if encoded == "" {
			missing = append(missing, part)
			continue
		}
		inputs[strconv.Itoa(part)] = encoded
	}
	cookie, err := loadSession()
	if err != nil {
		return err
	}
	// Keys need no seed. Fetch them alongside any missing encoded inputs.
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

	if len(missing) > 0 {
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
		var downloaded map[string]string
		if err := getJSON(ctx, inputURL, cookie, &downloaded); err != nil {
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
		for _, part := range missing {
			encoded, ok := downloaded[strconv.Itoa(part)]
			if !ok || encoded == "" {
				return fmt.Errorf("input response is missing encoded notes for part %d", part)
			}
			path := filepath.Join(partInputDir(dir, part), "input-encoded.txt")
			if err := replaceFile(root, path, []byte(encoded)); err != nil {
				return fmt.Errorf("write encoded notes for part %d: %w", part, err)
			}
			inputs[strconv.Itoa(part)] = encoded
		}
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
		write := writeNotes
		if force {
			write = replaceFile
		}
		if err := write(root, filepath.Join(inputDir, "input.txt"), notes); err != nil {
			return fmt.Errorf("write part %d notes: %w", part, err)
		}
		if _, err := fmt.Fprintf(output, "Part %d: decoded notes saved\n", part); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output, "Initialized %s\n", displayDir)
	return err
}

func questDownloadError(q quest, action string, err error) error {
	if questUnavailable(err) {
		return fmt.Errorf("%s: year %s or quest %d does not exist or is not available yet: %w", action, q.year, q.number, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func questUnavailable(err error) bool {
	var status *httpStatusError
	return errors.As(err, &status) && (status.statusCode == http.StatusNotFound || status.statusCode == http.StatusUnprocessableEntity)
}

func partInputDir(dir string, part int) string {
	return filepath.Join(dir, fmt.Sprintf("part-%02d", part), "input")
}

// Cache entries are reusable only when they are regular files containing
// complete hexadecimal AES blocks. Missing or malformed caches are downloaded
// again; filesystem failures are reported instead of silently ignored.
func readEncoded(root *os.Root, path string) (string, error) {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("input-encoded.txt must be a regular file")
	}
	f, err := root.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxJSONResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxJSONResponseBytes {
		return "", nil
	}
	encoded := strings.TrimSpace(string(data))
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded)%aes.BlockSize != 0 {
		return "", nil
	}
	return encoded, nil
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
		return &httpStatusError{endpoint: endpoint, statusCode: resp.StatusCode, status: resp.Status}
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

// Existing answer entries, including directories and dangling symlinks, are
// always preserved. Exclusive creation also protects against concurrent runs.
func createAnswer(root *os.Root, path string) error {
	if _, err := root.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeNotes(root, path, nil)
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
