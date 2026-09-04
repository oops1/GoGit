package settings

import (
	"testing"

	"github.com/oops1/gogit/internal/config"
)

func TestFromConfigCopiesFieldsAndNormalizes(t *testing.T) {
	cfg := config.Default()
	cfg.Language = "ru"
	cfg.Theme = config.ThemeDark
	cfg.UI.ShowToolbar = false
	cfg.UI.ShowStatusBar = false
	cfg.UI.JournalFullAuthorName = true
	cfg.Git.LogMaxCount = 750
	cfg.Git.AutoFetch = true
	cfg.Git.FetchInterval = 120
	cfg.Git.WorkTreeDepth = 6

	m := FromConfig(cfg)

	want := Model{
		Language:              "ru",
		Theme:                 config.ThemeDark,
		ShowToolbar:           false,
		ToolbarCaptions:       true,
		ShowStatusBar:         false,
		JournalFullAuthorName: true,
		LogMaxCount:           750,
		AutoFetch:             true,
		FetchInterval:         120,
		WorkTreeDepth:         6,
	}
	if m != want {
		t.Fatalf("model = %+v, want %+v", m, want)
	}
}

func TestFromConfigNormalizesOutOfRangeStoredValues(t *testing.T) {
	cfg := config.Default()
	cfg.Language = ""
	cfg.Theme = "bogus"
	cfg.Git.LogMaxCount = 1
	cfg.Git.FetchInterval = 1
	cfg.Git.WorkTreeDepth = MaxWorkTreeDepth + 1

	m := FromConfig(cfg)

	if m.Language != "en" {
		t.Fatalf("language = %q, want en", m.Language)
	}
	if m.Theme != config.ThemeSystem {
		t.Fatalf("theme = %q, want system", m.Theme)
	}
	if m.LogMaxCount != MinLogMaxCount {
		t.Fatalf("logMaxCount = %d, want %d", m.LogMaxCount, MinLogMaxCount)
	}
	if m.FetchInterval != MinFetchInterval {
		t.Fatalf("fetchInterval = %d, want %d", m.FetchInterval, MinFetchInterval)
	}
	if m.WorkTreeDepth != MaxWorkTreeDepth {
		t.Fatalf("workTreeDepth = %d, want %d", m.WorkTreeDepth, MaxWorkTreeDepth)
	}
}

func TestNormalizedDefaultsEmptyLanguageToEnglish(t *testing.T) {
	m := Model{}.Normalized()
	if m.Language != "en" {
		t.Fatalf("language = %q, want en", m.Language)
	}
}

func TestNormalizedKeepsNonEmptyLanguage(t *testing.T) {
	m := Model{Language: "fr"}.Normalized()
	if m.Language != "fr" {
		t.Fatalf("language = %q, want fr", m.Language)
	}
}

func TestNormalizedKeepsDarkAndLightThemes(t *testing.T) {
	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		m := Model{Theme: theme}.Normalized()
		if m.Theme != theme {
			t.Fatalf("theme = %q, want %q", m.Theme, theme)
		}
	}
}

func TestNormalizedFallsBackToSystemThemeForUnknownValue(t *testing.T) {
	for _, theme := range []string{"", "bogus", config.ThemeSystem} {
		m := Model{Theme: theme}.Normalized()
		if m.Theme != config.ThemeSystem {
			t.Fatalf("theme(%q) = %q, want system", theme, m.Theme)
		}
	}
}

func TestNormalizedClampsLogMaxCount(t *testing.T) {
	cases := map[string]struct {
		in   int
		want int
	}{
		"below minimum": {in: MinLogMaxCount - 1, want: MinLogMaxCount},
		"at minimum":    {in: MinLogMaxCount, want: MinLogMaxCount},
		"in range":      {in: 5000, want: 5000},
		"at maximum":    {in: MaxLogMaxCount, want: MaxLogMaxCount},
		"above maximum": {in: MaxLogMaxCount + 1, want: MaxLogMaxCount},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := Model{LogMaxCount: tc.in}.Normalized()
			if m.LogMaxCount != tc.want {
				t.Fatalf("logMaxCount = %d, want %d", m.LogMaxCount, tc.want)
			}
		})
	}
}

func TestNormalizedClampsFetchInterval(t *testing.T) {
	cases := map[string]struct {
		in   int
		want int
	}{
		"below minimum": {in: MinFetchInterval - 1, want: MinFetchInterval},
		"at minimum":    {in: MinFetchInterval, want: MinFetchInterval},
		"in range":      {in: 600, want: 600},
		"at maximum":    {in: MaxFetchInterval, want: MaxFetchInterval},
		"above maximum": {in: MaxFetchInterval + 1, want: MaxFetchInterval},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := Model{FetchInterval: tc.in}.Normalized()
			if m.FetchInterval != tc.want {
				t.Fatalf("fetchInterval = %d, want %d", m.FetchInterval, tc.want)
			}
		})
	}
}

func TestApplyToWritesNormalizedFieldsIntoConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "keep", Path: "keep"}}

	m := Model{
		Language:              "ru",
		Theme:                 "bogus",
		ShowToolbar:           false,
		ShowStatusBar:         false,
		JournalFullAuthorName: true,
		LogMaxCount:           MaxLogMaxCount + 1000,
		AutoFetch:             true,
		FetchInterval:         MinFetchInterval - 5,
		WorkTreeDepth:         -7,
	}
	m.ApplyTo(cfg)

	if cfg.Language != "ru" {
		t.Fatalf("language = %q", cfg.Language)
	}
	if cfg.Theme != config.ThemeSystem {
		t.Fatalf("theme = %q, want system", cfg.Theme)
	}
	if cfg.UI.ShowToolbar || cfg.UI.ShowStatusBar {
		t.Fatal("toolbar and status bar must be hidden")
	}
	if !cfg.UI.JournalFullAuthorName {
		t.Fatal("journal full author name must be enabled")
	}
	if cfg.Git.LogMaxCount != MaxLogMaxCount {
		t.Fatalf("logMaxCount = %d, want %d", cfg.Git.LogMaxCount, MaxLogMaxCount)
	}
	if !cfg.Git.AutoFetch {
		t.Fatal("auto-fetch must be true")
	}
	if cfg.Git.FetchInterval != MinFetchInterval {
		t.Fatalf("fetchInterval = %d, want %d", cfg.Git.FetchInterval, MinFetchInterval)
	}
	if cfg.Git.WorkTreeDepth != MinWorkTreeDepth {
		t.Fatalf("workTreeDepth = %d, want %d", cfg.Git.WorkTreeDepth, MinWorkTreeDepth)
	}
	if len(cfg.Repositories) != 1 || cfg.Repositories[0].ID != "r1" {
		t.Fatal("ApplyTo must not touch unrelated config fields")
	}
}

func TestApplyToRoundTripsWithFromConfig(t *testing.T) {
	src := config.Default()
	src.Language = "ru"
	src.Theme = config.ThemeLight
	src.UI.ShowToolbar = false
	src.UI.ShowStatusBar = true
	src.UI.JournalFullAuthorName = true
	src.Git.LogMaxCount = 42000
	src.Git.AutoFetch = true
	src.Git.FetchInterval = 900
	src.Git.WorkTreeDepth = 8

	m := FromConfig(src)

	dst := config.Default()
	m.ApplyTo(dst)

	if dst.Language != src.Language || dst.Theme != src.Theme {
		t.Fatalf("language/theme mismatch: %+v vs %+v", dst, src)
	}
	if dst.UI.ShowToolbar != src.UI.ShowToolbar || dst.UI.ShowStatusBar != src.UI.ShowStatusBar {
		t.Fatalf("ui mismatch: %+v vs %+v", dst.UI, src.UI)
	}
	if dst.UI.JournalFullAuthorName != src.UI.JournalFullAuthorName {
		t.Fatalf("journal full author name mismatch: %+v vs %+v", dst.UI, src.UI)
	}
	if dst.Git.LogMaxCount != src.Git.LogMaxCount || dst.Git.AutoFetch != src.Git.AutoFetch ||
		dst.Git.FetchInterval != src.Git.FetchInterval || dst.Git.WorkTreeDepth != src.Git.WorkTreeDepth {
		t.Fatalf("git mismatch: %+v vs %+v", dst.Git, src.Git)
	}
}

func TestNormalizedClampsWorkTreeDepth(t *testing.T) {
	cases := map[string]struct {
		in   int
		want int
	}{
		"below minimum": {in: MinWorkTreeDepth - 1, want: MinWorkTreeDepth},
		"at minimum":    {in: MinWorkTreeDepth, want: MinWorkTreeDepth},
		"in range":      {in: 10, want: 10},
		"at maximum":    {in: MaxWorkTreeDepth, want: MaxWorkTreeDepth},
		"above maximum": {in: MaxWorkTreeDepth + 1, want: MaxWorkTreeDepth},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := Model{WorkTreeDepth: tc.in}.Normalized()
			if m.WorkTreeDepth != tc.want {
				t.Fatalf("workTreeDepth = %d, want %d", m.WorkTreeDepth, tc.want)
			}
		})
	}
}
