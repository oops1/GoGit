package settings

import "github.com/oops1/gogit/internal/config"

const (
	MinLogMaxCount = 50
	MaxLogMaxCount = 100000

	MinFetchInterval = 30
	MaxFetchInterval = 3600

	MinWorkTreeDepth = 0
	MaxWorkTreeDepth = 100

	MinShallowDepth = 0
	MaxShallowDepth = 100000
)

type Model struct {
	Language              string
	Theme                 string
	ShowToolbar           bool
	ToolbarCaptions       bool
	ShowStatusBar         bool
	JournalFullAuthorName bool
	LogMaxCount           int
	AutoFetch             bool
	FetchInterval         int
	WorkTreeDepth         int
	PullStrategy          string
	DefaultRemote         string
	PruneOnFetch          bool
	BanAttribution        bool
	ShallowDepth          int
	CredentialSource      string
}

func FromConfig(cfg *config.Config) Model {
	m := Model{
		Language:              cfg.Language,
		Theme:                 cfg.Theme,
		ShowToolbar:           cfg.UI.ShowToolbar,
		ToolbarCaptions:       cfg.UI.ToolbarCaptions,
		ShowStatusBar:         cfg.UI.ShowStatusBar,
		JournalFullAuthorName: cfg.UI.JournalFullAuthorName,
		LogMaxCount:           cfg.Git.LogMaxCount,
		AutoFetch:             cfg.Git.AutoFetch,
		FetchInterval:         cfg.Git.FetchInterval,
		WorkTreeDepth:         cfg.Git.WorkTreeDepth,
		PullStrategy:          cfg.Git.PullStrategy,
		DefaultRemote:         cfg.Git.DefaultRemote,
		PruneOnFetch:          cfg.Git.PruneOnFetch,
		BanAttribution:        cfg.Git.BanAttribution,
		ShallowDepth:          cfg.Git.ShallowDepth,
		CredentialSource:      cfg.Git.CredentialSource,
	}
	return m.Normalized()
}

func (m Model) Normalized() Model {
	if m.Language == "" {
		m.Language = "en"
	}
	switch m.Theme {
	case config.ThemeDark, config.ThemeLight:
	default:
		m.Theme = config.ThemeSystem
	}
	m.LogMaxCount = clamp(m.LogMaxCount, MinLogMaxCount, MaxLogMaxCount)
	m.FetchInterval = clamp(m.FetchInterval, MinFetchInterval, MaxFetchInterval)
	m.WorkTreeDepth = clamp(m.WorkTreeDepth, MinWorkTreeDepth, MaxWorkTreeDepth)
	switch m.PullStrategy {
	case config.PullStrategyMerge, config.PullStrategyRebase:
	default:
		m.PullStrategy = config.PullStrategyFF
	}
	if m.DefaultRemote == "" {
		m.DefaultRemote = "origin"
	}
	m.ShallowDepth = clamp(m.ShallowDepth, MinShallowDepth, MaxShallowDepth)
	switch m.CredentialSource {
	case config.CredentialSourceVaultThenHelper, config.CredentialSourceHelper:
	default:
		m.CredentialSource = config.CredentialSourceVault
	}
	return m
}

func (m Model) ApplyTo(cfg *config.Config) {
	n := m.Normalized()
	cfg.Language = n.Language
	cfg.Theme = n.Theme
	cfg.UI.ShowToolbar = n.ShowToolbar
	cfg.UI.ToolbarCaptions = n.ToolbarCaptions
	cfg.UI.ShowStatusBar = n.ShowStatusBar
	cfg.UI.JournalFullAuthorName = n.JournalFullAuthorName
	cfg.Git.LogMaxCount = n.LogMaxCount
	cfg.Git.AutoFetch = n.AutoFetch
	cfg.Git.FetchInterval = n.FetchInterval
	cfg.Git.WorkTreeDepth = n.WorkTreeDepth
	cfg.Git.PullStrategy = n.PullStrategy
	cfg.Git.DefaultRemote = n.DefaultRemote
	cfg.Git.PruneOnFetch = n.PruneOnFetch
	cfg.Git.BanAttribution = n.BanAttribution
	cfg.Git.ShallowDepth = n.ShallowDepth
	cfg.Git.CredentialSource = n.CredentialSource
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
