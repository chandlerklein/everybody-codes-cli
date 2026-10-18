package everybodycodes

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

// Quest identifies a quest within a year or event.
type Quest struct {
	Year   string
	Number int
}

// ParseQuestFlags validates the year and decimal quest number.
func ParseQuestFlags(year, rawNumber string) (Quest, error) {
	if !yearID.MatchString(year) {
		return Quest{}, errors.New("year must be a nonempty identifier containing only letters, digits, hyphens, or underscores, starting with a letter or digit")
	}
	number, err := strconv.Atoi(rawNumber)
	if err != nil || number < 1 || !decimalQuestNumber.MatchString(rawNumber) {
		return Quest{}, errors.New("quest number must be a positive decimal integer (for example, 1 or 01)")
	}
	return Quest{Year: year, Number: number}, nil
}

// ParseQuestURL parses an Everybody Codes quest URL.
func ParseQuestURL(raw string) (Quest, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "everybody.codes" || u.User != nil {
		return Quest{}, errors.New("expected a quest URL: https://everybody.codes/event/<YEAR>/quests/<QUEST>")
	}
	match := questPath.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return Quest{}, errors.New("expected a quest URL: https://everybody.codes/event/<YEAR>/quests/<QUEST>")
	}
	number, err := strconv.Atoi(match[2])
	if err != nil || number < 1 {
		return Quest{}, errors.New("quest number must be a positive integer")
	}
	return Quest{Year: match[1], Number: number}, nil
}

func partDir(dir string, part int) string {
	return filepath.Join(dir, fmt.Sprintf("part-%02d", part))
}
