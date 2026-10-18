package main

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
	statusPartName  = regexp.MustCompile(`^part-([0-9]+)$`)
)

type localQuestStatus struct {
	name   string
	number int
	ready  bool
}

type localEventStatus struct {
	name   string
	quests []localQuestStatus
}

func runStatus(baseDir string, output io.Writer) error {
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
			ready, err := questIsReady(root, filepath.Join(eventName, questName))
			if err != nil {
				return err
			}
			event.quests = append(event.quests, localQuestStatus{name: questName, number: number, ready: ready})
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
			state := "locked"
			if quest.ready {
				state = "ready"
			}
			if _, err := fmt.Fprintf(output, "  %s: %s\n", quest.name, state); err != nil {
				return err
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

func questIsReady(root *os.Root, path string) (bool, error) {
	parts, err := statusDirectories(root, path, statusPartName)
	if err != nil {
		return false, err
	}
	ready := false
	for _, part := range parts {
		number, err := strconv.Atoi(statusPartName.FindStringSubmatch(part)[1])
		if err != nil || number < 1 {
			continue
		}
		inputPath := filepath.Join(path, part, "input", "input.txt")
		info, err := root.Stat(inputPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("check decoded input %s: %w", inputPath, err)
		}
		if info.Mode().IsRegular() {
			ready = true
		}
	}
	return ready, nil
}
