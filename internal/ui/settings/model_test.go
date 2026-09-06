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
	cfg.Git.PullStrategy = config.PullStrategyRebase
	cfg.Git.DefaultRemote = "upstream"
	cfg.Git.PruneOnFetch = true
	cfg.Git.ShallowDepth = 20
	cfg.Git.CredentialSource = config.CredentialSourceHelper

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
		PullStrategy:          config.PullStrategyRebase,
		DefaultRemote:         "upstream",
		PruneOnFetch:          true,
		ShallowDepth:          20,
		CredentialSource:      config.CredentialSourceHelper,
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
	cfg.Git.PullStrategy = "bogus"
	cfg.Git.DefaultRemote = ""
	cfg.Git.ShallowDepth = MaxShallowDepth + 1
	cfg.Git.CredentialSource = "bogus"

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
	if m.PullStrategy != config.PullStrategyFF {
		t.Fatalf("pullStrategy = %q, want %q", m.PullStrategy, config.PullStrategyFF)
	}
	if m.DefaultRemote != "origin" {
		t.Fatalf("defaultRemote = %q, want origin", m.DefaultRemote)
	}
	if m.ShallowDepth != MaxShallowDepth {
		t.Fatalf("shallowDepth = %d, want %d", m.ShallowDepth, MaxShallowDepth)
	}
	if m.CredentialSource != config.CredentialSourceVault {
		t.Fatalf("credentialSource = %q, want %q", m.CredentialSource, config.CredentialSourceVault)
	}
}

func TestNormalizedKeepsValidCredentialSources(t *testing.T) {
	for _, source := range []string{config.CredentialSourceVault, config.CredentialSourceVaultThenHelper, config.CredentialSourceHelper} {
		m := Model{CredentialSource: source}.Normalized()
		if m.CredentialSource != source {
			t.Fatalf("credentialSource = %q, want %q", m.CredentialSource, source)
		}
	}
}

func TestNormalizedFallsBackToVaultForUnknownCredentialSource(t *testing.T) {
	for _, source := range []string{"", "bogus", "Vault"} {
		m := Model{CredentialSource: source}.Normalized()
		if m.CredentialSource != config.CredentialSourceVault {
			t.Fatalf("credentialSource(%q) = %q, want %q", source, m.CredentialSource, config.CredentialSourceVault)
		}
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

func TestNormalizedKeepsValidPullStrategies(t *testing.T) {
	for _, strategy := range []string{config.PullStrategyFF, config.PullStrategyMerge, config.PullStrategyRebase} {
		m := Model{PullStrategy: strategy}.Normalized()
		if m.PullStrategy != strategy {
			t.Fatalf("pullStrategy = %q, want %q", m.PullStrategy, strategy)
		}
	}
}

func TestNormalizedFallsBackToFFForUnknownPullStrategy(t *testing.T) {
	for _, strategy := range []string{"", "bogus", "FF"} {
		m := Model{PullStrategy: strategy}.Normalized()
		if m.PullStrategy != config.PullStrategyFF {
			t.Fatalf("pullStrategy(%q) = %q, want %q", strategy, m.PullStrategy, config.PullStrategyFF)
		}
	}
}

func TestNormalizedFallsBackToOriginForEmptyDefaultRemote(t *testing.T) {
	m := Model{DefaultRemote: ""}.Normalized()
	if m.DefaultRemote != "origin" {
		t.Fatalf("defaultRemote = %q, want origin", m.DefaultRemote)
	}
}

func TestNormalizedKeepsNonEmptyDefaultRemote(t *testing.T) {
	m := Model{DefaultRemote: "upstream"}.Normalized()
	if m.DefaultRemote != "upstream" {
		t.Fatalf("defaultRemote = %q, want upstream", m.DefaultRemote)
	}
}

func TestNormalizedClampsShallowDepth(t *testing.T) {
	cases := map[string]struct {
		in   int
		want int
	}{
		"below minimum": {in: MinShallowDepth - 1, want: MinShallowDepth},
		"at minimum":    {in: MinShallowDepth, want: MinShallowDepth},
		"in range":      {in: 50, want: 50},
		"at maximum":    {in: MaxShallowDepth, want: MaxShallowDepth},
		"above maximum": {in: MaxShallowDepth + 1, want: MaxShallowDepth},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := Model{ShallowDepth: tc.in}.Normalized()
			if m.ShallowDepth != tc.want {
				t.Fatalf("shallowDepth = %d, want %d", m.ShallowDepth, tc.want)
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
		PullStrategy:          "bogus",
		DefaultRemote:         "",
		PruneOnFetch:          true,
		ShallowDepth:          -3,
		CredentialSource:      "bogus",
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
	if cfg.Git.PullStrategy != config.PullStrategyFF {
		t.Fatalf("pullStrategy = %q, want %q", cfg.Git.PullStrategy, config.PullStrategyFF)
	}
	if cfg.Git.DefaultRemote != "origin" {
		t.Fatalf("defaultRemote = %q, want origin", cfg.Git.DefaultRemote)
	}
	if !cfg.Git.PruneOnFetch {
		t.Fatal("pruneOnFetch must be true")
	}
	if cfg.Git.ShallowDepth != MinShallowDepth {
		t.Fatalf("shallowDepth = %d, want %d", cfg.Git.ShallowDepth, MinShallowDepth)
	}
	if cfg.Git.CredentialSource != config.CredentialSourceVault {
		t.Fatalf("credentialSource = %q, want %q", cfg.Git.CredentialSource, config.CredentialSourceVault)
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
	src.Git.PullStrategy = config.PullStrategyMerge
	src.Git.DefaultRemote = "upstream"
	src.Git.PruneOnFetch = true
	src.Git.ShallowDepth = 30
	src.Git.CredentialSource = config.CredentialSourceVaultThenHelper

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
	if dst.Git.PullStrategy != src.Git.PullStrategy || dst.Git.DefaultRemote != src.Git.DefaultRemote ||
		dst.Git.PruneOnFetch != src.Git.PruneOnFetch || dst.Git.ShallowDepth != src.Git.ShallowDepth {
		t.Fatalf("git network mismatch: %+v vs %+v", dst.Git, src.Git)
	}
	if dst.Git.CredentialSource != src.Git.CredentialSource {
		t.Fatalf("credentialSource mismatch: %q vs %q", dst.Git.CredentialSource, src.Git.CredentialSource)
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
