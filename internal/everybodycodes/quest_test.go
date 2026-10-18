package everybodycodes

import (
	"testing"
)

func TestParseQuestURL(t *testing.T) {
	for _, tt := range []struct {
		url, year string
		number    int
	}{
		{testURL, "2024", 1},
		{"https://everybody.codes/event/2024/quests/12/", "2024", 12},
		{"https://everybody.codes/event/2024/quests/01?foo=bar#part1", "2024", 1},
	} {
		got, err := ParseQuestURL(tt.url)
		if err != nil || got.Year != tt.year || got.Number != tt.number {
			t.Errorf("parse %s = %v, %v", tt.url, got, err)
		}
	}
	for _, raw := range []string{
		"", "not a URL", "http://everybody.codes/event/2024/quests/1",
		"https://example.com/event/2024/quests/1",
		"https://everybody.codes.evil.example/event/2024/quests/1",
		"https://user:pass@everybody.codes/event/2024/quests/1",
		"https://everybody.codes/event/../quests/1",
		"https://everybody.codes/event/%2e%2e/quests/1",
		"https://everybody.codes/event/2024/quests/0",
		"https://everybody.codes/event/2024/quests/-1",
		"https://everybody.codes/event/2024/quests/99999999999999999999",
		"https://everybody.codes/event/2024/quests/one",
		"https://everybody.codes/event/2024/quests/1/extra",
	} {
		if _, err := ParseQuestURL(raw); err == nil {
			t.Errorf("accepted invalid URL %q", raw)
		}
	}
}
