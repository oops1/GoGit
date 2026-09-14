package ops

import (
	"errors"
	"testing"
	"time"
)

func TestExpiryDatesReadTheWayGitWritesThem(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		value   string
		want    time.Time
		expires bool
	}{
		{"never", time.Time{}, false},
		{"false", time.Time{}, false},
		{"now", now, true},
		{"all", now, true},
		{" Now ", now, true},
		{"2026-01-31", time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), true},
		{"30.seconds.ago", now.Add(-30 * time.Second), true},
		{"1.minute.ago", now.Add(-time.Minute), true},
		{"12.hours.ago", now.Add(-12 * time.Hour), true},
		{"3 days ago", now.AddDate(0, 0, -3), true},
		{"2.weeks.ago", now.AddDate(0, 0, -14), true},
		{"1.month.ago", now.AddDate(0, -1, 0), true},
		{"2.years.ago", now.AddDate(-2, 0, 0), true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, expires, err := parseExpiry(tt.value, now)
			if err != nil || expires != tt.expires || !got.Equal(tt.want) {
				t.Fatalf("parseExpiry(%q) = %v, %v, %v, want %v, %v", tt.value, got, expires, err, tt.want, tt.expires)
			}
		})
	}
}

func TestExpiryDatesGitWouldNotUnderstandAreRefused(t *testing.T) {
	now := time.Now()
	for _, value := range []string{"", "soon", "2.weeks", "two.weeks.ago", "-1.days.ago", "3.fortnights.ago", "1.2.3.ago"} {
		t.Run(value, func(t *testing.T) {
			if _, _, err := parseExpiry(value, now); !errors.Is(err, ErrInvalidExpiry) {
				t.Fatalf("parseExpiry(%q) err = %v, want ErrInvalidExpiry", value, err)
			}
		})
	}
}
