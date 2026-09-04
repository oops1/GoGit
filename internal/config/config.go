package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 1

const (
	ThemeSystem = "system"
	ThemeDark   = "dark"
	ThemeLight  = "light"
)

const (
	MinWindowWidth  = 800
	MinWindowHeight = 600
)

type Config struct {
	Version          int          `toml:"version"`
	Language         string       `toml:"language"`
	Theme            string       `toml:"theme"`
	Window           Window       `toml:"window"`
	Git              Git          `toml:"git"`
	UI               UI           `toml:"ui"`
	Groups           []Group      `toml:"groups"`
	Repositories     []Repository `toml:"repositories"`
	ActiveRepository string       `toml:"active_repository"`
}

type Window struct {
	Width     int  `toml:"width"`
	Height    int  `toml:"height"`
	X         int  `toml:"x"`
	Y         int  `toml:"y"`
	Maximized bool `toml:"maximized"`
}

type Git struct {
	Executable    string `toml:"executable"`
	LogMaxCount   int    `toml:"log_max_count"`
	AutoFetch     bool   `toml:"auto_fetch"`
	FetchInterval int    `toml:"fetch_interval_sec"`
	WorkTreeDepth int    `toml:"worktree_scan_depth"`
}

type UI struct {
	ShowToolbar           bool     `toml:"show_toolbar"`
	ToolbarCaptions       bool     `toml:"toolbar_captions"`
	ShowStatusBar         bool     `toml:"show_status_bar"`
	FilesColumns          []string `toml:"files_columns"`
	FilesVisibleColumns   []string `toml:"files_visible_columns"`
	FilesStatusFilter     []string `toml:"files_status_filter"`
	FilesSubdirectories   bool     `toml:"files_subdirectories"`
	JournalFullAuthorName bool     `toml:"journal_full_author_name"`
}

type Group struct {
	ID     string `toml:"id"`
	Name   string `toml:"name"`
	Parent string `toml:"parent"`
}

type Repository struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Path     string `toml:"path"`
	Group    string `toml:"group"`
	Worktree bool   `toml:"worktree"`
	Parent   string `toml:"parent"`
}

var ErrUnsupportedVersion = errors.New("config: unsupported version")

func Default() *Config {
	return &Config{
		Version:  CurrentVersion,
		Language: "en",
		Theme:    ThemeSystem,
		Window:   Window{Width: 1280, Height: 800},
		Git:      Git{LogMaxCount: 500, FetchInterval: 300},
		UI:       UI{ShowToolbar: true, ShowStatusBar: true, ToolbarCaptions: true, FilesSubdirectories: true},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	cfg := Default()
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Version > CurrentVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, cfg.Version)
	}
	cfg.Version = CurrentVersion
	cfg.Normalize()
	return cfg, nil
}

func (c *Config) Normalize() {
	if c.Language == "" {
		c.Language = "en"
	}
	if c.Theme != ThemeLight && c.Theme != ThemeDark {
		c.Theme = ThemeSystem
	}
	if c.Window.Width < MinWindowWidth {
		c.Window.Width = MinWindowWidth
	}
	if c.Window.Height < MinWindowHeight {
		c.Window.Height = MinWindowHeight
	}
	if c.Git.LogMaxCount <= 0 {
		c.Git.LogMaxCount = 500
	}
	if c.Git.FetchInterval <= 0 {
		c.Git.FetchInterval = 300
	}
	if c.Git.WorkTreeDepth < 0 {
		c.Git.WorkTreeDepth = 0
	}
}

func (c *Config) Encode() ([]byte, error) {
	var buf bytes.Buffer
	err := toml.NewEncoder(&buf).Encode(c)
	return buf.Bytes(), err
}

func (c *Config) Save(path string) error {
	data, err := c.Encode()
	if err == nil {
		err = writeAtomic(path, data)
	}
	return err
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) FindRepository(id string) (Repository, bool) {
	for _, r := range c.Repositories {
		if r.ID == id {
			return r, true
		}
	}
	return Repository{}, false
}

func (c *Config) AddRepository(r Repository) bool {
	for _, existing := range c.Repositories {
		if existing.ID == r.ID || filepath.Clean(existing.Path) == filepath.Clean(r.Path) {
			return false
		}
	}
	c.Repositories = append(c.Repositories, r)
	return true
}

func (c *Config) RemoveRepository(id string) bool {
	for i, r := range c.Repositories {
		if r.ID == id {
			c.Repositories = append(c.Repositories[:i], c.Repositories[i+1:]...)
			return true
		}
	}
	return false
}

func (c *Config) AddGroup(g Group) bool {
	for _, existing := range c.Groups {
		if existing.ID == g.ID {
			return false
		}
	}
	c.Groups = append(c.Groups, g)
	return true
}

func (c *Config) RemoveGroup(id string) bool {
	idx := -1
	for i, g := range c.Groups {
		if g.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	c.Groups = append(c.Groups[:idx], c.Groups[idx+1:]...)
	for i := range c.Groups {
		if c.Groups[i].Parent == id {
			c.Groups[i].Parent = ""
		}
	}
	for i := range c.Repositories {
		if c.Repositories[i].Group == id {
			c.Repositories[i].Group = ""
		}
	}
	return true
}
