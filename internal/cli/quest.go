package cli

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
)

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

func partDir(dir string, part int) string {
	return filepath.Join(dir, fmt.Sprintf("part-%02d", part))
}
