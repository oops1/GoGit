package gitversion

import (
	"strconv"
	"strings"
)

func Parse(text string) (major, minor int, ok bool) {
	for field := range strings.FieldsSeq(text) {
		if field == "" || field[0] < '0' || field[0] > '9' {
			continue
		}
		parts := strings.SplitN(field, ".", 3)
		if len(parts) < 2 {
			continue
		}
		major, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		return major, minor, true
	}
	return 0, 0, false
}

func AtLeast(text string, major, minor int) bool {
	gotMajor, gotMinor, ok := Parse(text)
	if !ok {
		return true
	}
	if gotMajor != major {
		return gotMajor > major
	}
	return gotMinor >= minor
}
