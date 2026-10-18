package everybodycodes

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

var (
	statusEventName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	statusQuestName = regexp.MustCompile(`^quest-([0-9]+)$`)
	statusPartName  = regexp.MustCompile(`^part-(0[1-3])$`)
)

type localQuestStatus struct {
	name   string
	number int
	parts  [3]bool
}

type localEventStatus struct {
	name   string
	quests []localQuestStatus
}

// Status prints the readiness of locally saved quest inputs.
func Status(baseDir string, output io.Writer) error {
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return fmt.Errorf("open status directory: %w", err)
	}
	defer root.Close()
	eventNames, err := statusDirectories(root, ".", statusEventName)
	if err != nil {
		return err
	}
	var events []localEventStatus
	for _, eventName := range eventNames {
		questNames, err := statusDirectories(root, eventName, statusQuestName)
		if err != nil {
			return err
		}
		event := localEventStatus{name: eventName}
		for _, questName := range questNames {
			number, err := strconv.Atoi(statusQuestName.FindStringSubmatch(questName)[1])
			if err != nil || number < 1 {
				continue
			}
			parts, err := questPartReadiness(root, filepath.Join(eventName, questName))
			if err != nil {
				return err
			}
			event.quests = append(event.quests, localQuestStatus{name: questName, number: number, parts: parts})
		}
		if len(event.quests) == 0 {
			continue
		}
		sort.SliceStable(event.quests, func(i, j int) bool {
			return event.quests[i].number < event.quests[j].number
		})
		events = append(events, event)
	}
	if len(events) == 0 {
		_, err := fmt.Fprintln(output, "No local quests found.")
		return err
	}
	for _, event := range events {
		if _, err := fmt.Fprintln(output, event.name); err != nil {
			return err
		}
		for _, quest := range event.quests {
			if _, err := fmt.Fprintf(output, "  %s\n", quest.name); err != nil {
				return err
			}
			for index, ready := range quest.parts {
				state := "locked"
				if ready {
					state = "ready"
				}
				if _, err := fmt.Fprintf(output, "    part-%02d: %s\n", index+1, state); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Root operations allow links within baseDir and reject links that escape it.
func statusDirectories(root *os.Root, path string, pattern *regexp.Regexp) ([]string, error) {
	entries, err := fs.ReadDir(root.FS(), path)
	if err != nil {
		return nil, fmt.Errorf("read status directory %s: %w", path, err)
	}
	var names []string
	for _, entry := range entries {
		if !pattern.MatchString(entry.Name()) {
			continue
		}
		entryPath := filepath.Join(path, entry.Name())
		info, err := root.Stat(entryPath)
		if err != nil {
			return nil, fmt.Errorf("check status directory %s: %w", entryPath, err)
		}
		if info.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func questPartReadiness(root *os.Root, path string) ([3]bool, error) {
	var ready [3]bool
	parts, err := statusDirectories(root, path, statusPartName)
	if err != nil {
		return ready, err
	}
	for _, part := range parts {
		number, err := strconv.Atoi(statusPartName.FindStringSubmatch(part)[1])
		if err != nil || number < 1 || number > len(ready) {
			continue
		}
		inputPath := filepath.Join(partDir(path, number), "input.txt")
		info, err := root.Stat(inputPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ready, fmt.Errorf("check decoded input %s: %w", inputPath, err)
		}
		if info.Mode().IsRegular() {
			ready[number-1] = true
		}
	}
	return ready, nil
}
