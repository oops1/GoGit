package ops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidExpiry = errors.New("ops: invalid expiry date")

var expiryUnits = map[string]func(now time.Time, count int) time.Time{
	"second": func(now time.Time, count int) time.Time { return now.Add(-time.Duration(count) * time.Second) },
	"minute": func(now time.Time, count int) time.Time { return now.Add(-time.Duration(count) * time.Minute) },
	"hour":   func(now time.Time, count int) time.Time { return now.Add(-time.Duration(count) * time.Hour) },
	"day":    func(now time.Time, count int) time.Time { return now.AddDate(0, 0, -count) },
	"week":   func(now time.Time, count int) time.Time { return now.AddDate(0, 0, -7*count) },
	"month":  func(now time.Time, count int) time.Time { return now.AddDate(0, -count, 0) },
	"year":   func(now time.Time, count int) time.Time { return now.AddDate(-count, 0, 0) },
}

func parseExpiry(value string, now time.Time) (time.Time, bool, error) {
	text := strings.ToLower(strings.TrimSpace(value))
	switch text {
	case "never", "false":
		return time.Time{}, false, nil
	case "now", "all":
		return now, true, nil
	}
	if when, err := time.ParseInLocation(time.DateOnly, text, now.Location()); err == nil {
		return when, true, nil
	}
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == '.' || r == ' ' })
	if len(fields) != 3 || fields[2] != "ago" {
		return time.Time{}, false, fmt.Errorf("%w: %q", ErrInvalidExpiry, value)
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count < 0 {
		return time.Time{}, false, fmt.Errorf("%w: %q", ErrInvalidExpiry, value)
	}
	back, ok := expiryUnits[strings.TrimSuffix(fields[1], "s")]
	if !ok {
		return time.Time{}, false, fmt.Errorf("%w: %q", ErrInvalidExpiry, value)
	}
	return back(now, count), true, nil
}
