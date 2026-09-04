package settings

import "github.com/oops1/gogit/internal/config"

const (
	MinLogMaxCount = 50
	MaxLogMaxCount = 100000

	MinFetchInterval = 30
	MaxFetchInterval = 3600

	MinWorkTreeDepth = 0
	MaxWorkTreeDepth = 100
)

type Model struct {
	Language      string
	Theme         string
	ShowToolbar   bool
	ShowStatusBar bool
	LogMaxCount   int
	AutoFetch     bool
	FetchInterval int
	WorkTreeDepth int
}

func FromConfig(cfg *config.Config) Model {
	m := Model{
		Language:      cfg.Language,
		Theme:         cfg.Theme,
		ShowToolbar:   cfg.UI.ShowToolbar,
		ShowStatusBar: cfg.UI.ShowStatusBar,
		LogMaxCount:   cfg.Git.LogMaxCount,
		AutoFetch:     cfg.Git.AutoFetch,
		FetchInterval: cfg.Git.FetchInterval,
		WorkTreeDepth: cfg.Git.WorkTreeDepth,
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
	return m
}

func (m Model) ApplyTo(cfg *config.Config) {
	n := m.Normalized()
	cfg.Language = n.Language
	cfg.Theme = n.Theme
	cfg.UI.ShowToolbar = n.ShowToolbar
	cfg.UI.ShowStatusBar = n.ShowStatusBar
	cfg.Git.LogMaxCount = n.LogMaxCount
	cfg.Git.AutoFetch = n.AutoFetch
	cfg.Git.FetchInterval = n.FetchInterval
	cfg.Git.WorkTreeDepth = n.WorkTreeDepth
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
