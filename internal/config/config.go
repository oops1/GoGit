package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/oops1/gogit/internal/safefile"
)

const CurrentVersion = 1

const (
	ThemeSystem = "system"
	ThemeDark   = "dark"
	ThemeLight  = "light"
)

const (
	LayoutDocks   = "docks"
	LayoutSidebar = "sidebar"
)

const (
	PullStrategyFF     = "ff"
	PullStrategyMerge  = "merge"
	PullStrategyRebase = "rebase"
)

const (
	SwitchChangesAsk       = "ask"
	SwitchChangesStash     = "stash"
	SwitchChangesMerge     = "merge"
	SwitchChangesOverwrite = "overwrite"
)

const (
	CredentialSourceVault           = "vault"
	CredentialSourceVaultThenHelper = "vault+helper"
	CredentialSourceHelper          = "helper"
)

const (
	MinWindowWidth  = 1280
	MinWindowHeight = 760
)

type Config struct {
	Version          int          `toml:"version"`
	Language         string       `toml:"language"`
	Theme            string       `toml:"theme"`
	Window           Window       `toml:"window"`
	Git              Git          `toml:"git"`
	UI               UI           `toml:"ui"`
	Updates          Updates      `toml:"updates"`
	Security         Security     `toml:"security"`
	Groups           []Group      `toml:"groups"`
	Repositories     []Repository `toml:"repositories"`
	ActiveRepository string       `toml:"active_repository"`

	saved  savedState
	extras []extraValue
}

type savedState struct {
	sum          [sha256.Size]byte
	groups       []Group
	repositories []Repository
}

type Window struct {
	Width     int  `toml:"width"`
	Height    int  `toml:"height"`
	X         int  `toml:"x"`
	Y         int  `toml:"y"`
	Maximized bool `toml:"maximized"`
}

type Git struct {
	Executable       string `toml:"executable"`
	LogMaxCount      int    `toml:"log_max_count"`
	AutoFetch        bool   `toml:"auto_fetch"`
	FetchInterval    int    `toml:"fetch_interval_sec"`
	WorkTreeDepth    int    `toml:"worktree_scan_depth"`
	PullStrategy     string `toml:"pull_strategy"`
	DefaultRemote    string `toml:"default_remote"`
	PruneOnFetch     bool   `toml:"prune_on_fetch"`
	BanAttribution   bool   `toml:"ban_attribution"`
	ShallowDepth     int    `toml:"shallow_depth"`
	CredentialSource string `toml:"credential_source"`
	SwitchChanges    string `toml:"switch_local_changes"`
}

type UI struct {
	ShowToolbar           bool     `toml:"show_toolbar"`
	ToolbarCaptions       bool     `toml:"toolbar_captions"`
	ToolbarItems          []string `toml:"toolbar_items"`
	ShowStatusBar         bool     `toml:"show_status_bar"`
	FilesColumns          []string `toml:"files_columns"`
	FilesVisibleColumns   []string `toml:"files_visible_columns"`
	FilesStatusFilter     []string `toml:"files_status_filter"`
	FilesSubdirectories   bool     `toml:"files_subdirectories"`
	JournalFullAuthorName bool     `toml:"journal_full_author_name"`
	CollapsedGroups       []string `toml:"collapsed_groups"`
	Layout                string   `toml:"layout"`
}

type Updates struct {
	LastCheck time.Time `toml:"last_check"`
}

type Security struct {
	VaultGeneration uint64 `toml:"vault_generation"`
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

var (
	ErrUnsupportedVersion = errors.New("config: unsupported version")
	ErrInvalid            = errors.New("config: invalid file")
)

func Default() *Config {
	return &Config{
		Version:  CurrentVersion,
		Language: "en",
		Theme:    ThemeSystem,
		Window:   Window{Width: 1280, Height: 800},
		Git:      Git{LogMaxCount: 500, FetchInterval: 300, PullStrategy: PullStrategyFF, DefaultRemote: "origin", CredentialSource: CredentialSourceVault, BanAttribution: true, SwitchChanges: SwitchChangesAsk},
		UI:       UI{ShowToolbar: true, ShowStatusBar: true, ToolbarCaptions: true, FilesSubdirectories: true, Layout: LayoutDocks},
	}
}

func Load(path string) (*Config, error) {
	data, err := safefile.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func LoadOrRecover(path string, now time.Time) (*Config, string, error) {
	cfg, err := Load(path)
	if !errors.Is(err, ErrInvalid) {
		return cfg, "", err
	}
	backup := path + ".bad-" + now.Format("20060102-150405")
	if err := os.Rename(path, backup); err != nil {
		return nil, "", err
	}
	return Default(), backup, nil
}

func Parse(data []byte) (*Config, error) {
	cfg := Default()
	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if cfg.Version > CurrentVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, cfg.Version)
	}
	cfg.Version = CurrentVersion
	cfg.Normalize()
	var raw map[string]any
	_, _ = toml.Decode(string(data), &raw)
	cfg.extras = collectExtras(raw, meta.Undecoded())
	cfg.remember(data)
	return cfg, nil
}

func (c *Config) remember(data []byte) {
	c.saved = savedState{
		sum:          sha256.Sum256(data),
		groups:       slices.Clone(c.Groups),
		repositories: slices.Clone(c.Repositories),
	}
}

func (c *Config) Normalize() {
	if c.Language == "" {
		c.Language = "en"
	}
	if c.Theme != ThemeLight && c.Theme != ThemeDark {
		c.Theme = ThemeSystem
	}
	if c.UI.Layout != LayoutSidebar {
		c.UI.Layout = LayoutDocks
	}
	c.UI.ToolbarItems = trimmedList(c.UI.ToolbarItems)
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
	switch c.Git.PullStrategy {
	case PullStrategyMerge, PullStrategyRebase:
	default:
		c.Git.PullStrategy = PullStrategyFF
	}
	switch c.Git.SwitchChanges {
	case SwitchChangesStash, SwitchChangesMerge, SwitchChangesOverwrite:
	default:
		c.Git.SwitchChanges = SwitchChangesAsk
	}
	if c.Git.DefaultRemote == "" {
		c.Git.DefaultRemote = "origin"
	}
	if c.Git.ShallowDepth < 0 {
		c.Git.ShallowDepth = 0
	}
	switch c.Git.CredentialSource {
	case CredentialSourceVaultThenHelper, CredentialSourceHelper:
	default:
		c.Git.CredentialSource = CredentialSourceVault
	}
}

func (c *Config) Encode() ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil || len(c.extras) == 0 {
		return buf.Bytes(), err
	}
	var doc map[string]any
	_, _ = toml.Decode(buf.String(), &doc)
	for _, extra := range c.extras {
		insertExtra(doc, extra.path, extra.value)
	}
	buf.Reset()
	err := toml.NewEncoder(&buf).Encode(doc)
	return buf.Bytes(), err
}

func (c *Config) Save(path string) error {
	var data []byte
	err := safefile.Update(path, func(current []byte) ([]byte, error) {
		if err := c.mergeChanges(current); err != nil {
			return nil, err
		}
		var err error
		data, err = c.Encode()
		return data, err
	})
	if err == nil {
		c.remember(data)
	}
	return err
}

func (c *Config) mergeChanges(current []byte) error {
	if current == nil || sha256.Sum256(current) == c.saved.sum {
		return nil
	}
	disk, err := Parse(current)
	if errors.Is(err, ErrUnsupportedVersion) {
		return err
	}
	if err != nil {
		return nil
	}
	c.Groups = mergeByID(c.saved.groups, c.Groups, disk.Groups, func(g Group) string { return g.ID })
	c.Repositories = uniqueRepositoryPaths(mergeByID(c.saved.repositories, c.Repositories, disk.Repositories, func(r Repository) string { return r.ID }))
	c.Security.VaultGeneration = max(c.Security.VaultGeneration, disk.Security.VaultGeneration)
	c.extras = mergeExtras(c.extras, disk.extras)
	return nil
}

func trimmedList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeByID[T any](base, ours, theirs []T, id func(T) string) []T {
	inBase, inOurs, inTheirs := idSet(base, id), idSet(ours, id), idSet(theirs, id)
	var out []T
	for _, item := range ours {
		if !inBase[id(item)] || inTheirs[id(item)] {
			out = append(out, item)
		}
	}
	for _, item := range theirs {
		if !inBase[id(item)] && !inOurs[id(item)] {
			out = append(out, item)
		}
	}
	return out
}

func idSet[T any](items []T, id func(T) string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[id(item)] = true
	}
	return set
}

func uniqueRepositoryPaths(repos []Repository) []Repository {
	seen := make(map[string]bool, len(repos))
	return slices.DeleteFunc(repos, func(r Repository) bool {
		key := filepath.Clean(r.Path)
		if seen[key] {
			return true
		}
		seen[key] = true
		return false
	})
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
