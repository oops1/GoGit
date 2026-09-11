package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Language != "en" || cfg.Theme != ThemeSystem {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Window.Width < MinWindowWidth || cfg.Window.Height < MinWindowHeight {
		t.Fatalf("window smaller than minimum: %+v", cfg.Window)
	}
	if !cfg.UI.ShowToolbar || !cfg.UI.ShowStatusBar {
		t.Fatalf("ui defaults: %+v", cfg.UI)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" {
		t.Fatalf("language = %q", cfg.Language)
	}
}

func TestLoadReadError(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error reading a directory")
	}
}

func TestParseInvalidTOML(t *testing.T) {
	if _, err := Parse([]byte("language = [broken")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseUnsupportedVersion(t *testing.T) {
	_, err := Parse([]byte("version = 99"))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestParseNormalizes(t *testing.T) {
	cfg, err := Parse([]byte(`
version = 0
language = ""
theme = "neon"
[window]
width = 10
height = 10
[git]
log_max_count = -1
fetch_interval_sec = 0
worktree_scan_depth = -5
pull_strategy = "octopus"
default_remote = ""
shallow_depth = -3
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d", cfg.Version)
	}
	if cfg.Language != "en" || cfg.Theme != ThemeSystem {
		t.Fatalf("normalize language/theme: %+v", cfg)
	}
	if cfg.Window.Width != MinWindowWidth || cfg.Window.Height != MinWindowHeight {
		t.Fatalf("normalize window: %+v", cfg.Window)
	}
	if cfg.Git.LogMaxCount != 500 || cfg.Git.FetchInterval != 300 || cfg.Git.WorkTreeDepth != 0 {
		t.Fatalf("normalize git: %+v", cfg.Git)
	}
	if cfg.Git.PullStrategy != PullStrategyFF || cfg.Git.DefaultRemote != "origin" || cfg.Git.ShallowDepth != 0 {
		t.Fatalf("normalize git network settings: %+v", cfg.Git)
	}
}

func TestDefaultCredentialSourceIsVault(t *testing.T) {
	if got := Default().Git.CredentialSource; got != CredentialSourceVault {
		t.Fatalf("CredentialSource = %q, want %q", got, CredentialSourceVault)
	}
}

func TestNormalizeUnknownCredentialSourceFallsBackToVault(t *testing.T) {
	for _, value := range []string{"", "bogus", "Vault", "VAULT+HELPER"} {
		cfg := Default()
		cfg.Git.CredentialSource = value
		cfg.Normalize()
		if cfg.Git.CredentialSource != CredentialSourceVault {
			t.Fatalf("CredentialSource(%q) = %q, want %q", value, cfg.Git.CredentialSource, CredentialSourceVault)
		}
	}
}

func TestNormalizeKeepsValidCredentialSources(t *testing.T) {
	for _, value := range []string{CredentialSourceVault, CredentialSourceVaultThenHelper, CredentialSourceHelper} {
		cfg := Default()
		cfg.Git.CredentialSource = value
		cfg.Normalize()
		if cfg.Git.CredentialSource != value {
			t.Fatalf("CredentialSource(%q) = %q, want unchanged", value, cfg.Git.CredentialSource)
		}
	}
}

func TestDefaultWorkTreeDepthIsUnlimited(t *testing.T) {
	if got := Default().Git.WorkTreeDepth; got != 0 {
		t.Fatalf("WorkTreeDepth = %d, want 0 (unlimited)", got)
	}
}

func TestNormalizeKeepsAPositiveWorkTreeDepth(t *testing.T) {
	cfg := Default()
	cfg.Git.WorkTreeDepth = 4
	cfg.Normalize()
	if cfg.Git.WorkTreeDepth != 4 {
		t.Fatalf("WorkTreeDepth = %d, want 4", cfg.Git.WorkTreeDepth)
	}
}

func TestNormalizeClampsNegativeWorkTreeDepthToZero(t *testing.T) {
	cfg := Default()
	cfg.Git.WorkTreeDepth = -3
	cfg.Normalize()
	if cfg.Git.WorkTreeDepth != 0 {
		t.Fatalf("WorkTreeDepth = %d, want 0", cfg.Git.WorkTreeDepth)
	}
}

func TestParseKeepsExplicitThemes(t *testing.T) {
	for _, theme := range []string{ThemeLight, ThemeDark, ThemeSystem} {
		cfg, err := Parse([]byte(`theme = "` + theme + `"`))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Theme != theme {
			t.Fatalf("theme = %q, want %q", cfg.Theme, theme)
		}
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	cfg := Default()
	cfg.Language = "ru"
	cfg.Theme = ThemeLight
	cfg.Groups = []Group{{ID: "g1", Name: "Work"}}
	cfg.Repositories = []Repository{{ID: "r1", Name: "gogit", Path: `D:\Projects\gogit`, Group: "g1"}}
	cfg.UI.FilesStatusFilter = []string{"ignored", "untracked"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("empty config written")
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file left behind: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "ru" || loaded.Theme != ThemeLight {
		t.Fatalf("round trip: %+v", loaded)
	}
	if len(loaded.Repositories) != 1 || loaded.Repositories[0].Path != cfg.Repositories[0].Path {
		t.Fatalf("repositories: %+v", loaded.Repositories)
	}
	if len(loaded.Groups) != 1 || loaded.Groups[0].Name != "Work" {
		t.Fatalf("groups: %+v", loaded.Groups)
	}
	if !slices.Equal(loaded.UI.FilesStatusFilter, []string{"ignored", "untracked"}) {
		t.Fatalf("files status filter: %+v", loaded.UI.FilesStatusFilter)
	}
}

func TestJournalFullAuthorNameRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.UI.JournalFullAuthorName = true
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.UI.JournalFullAuthorName {
		t.Fatal("journal full author name must round trip as true")
	}
}

func TestJournalFullAuthorNameDefaultsToFalse(t *testing.T) {
	if Default().UI.JournalFullAuthorName {
		t.Fatal("journal full author name must default to false (initials badge)")
	}
}

func TestActiveRepositoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.Repositories = []Repository{{ID: "r1", Name: "gogit", Path: `D:\Projects\gogit`}}
	cfg.ActiveRepository = "r1"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveRepository != "r1" {
		t.Fatalf("active repository = %q, want %q", loaded.ActiveRepository, "r1")
	}
}

func TestSaveFailsWhenDirIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Default().Save(filepath.Join(blocker, "config.toml")); err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveFailsWhenTargetIsDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Default().Save(target); err == nil {
		t.Fatal("expected rename error")
	}
}

func TestSaveFailsWhenTempIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Default().Save(filepath.Join(dir, "config.toml")); err == nil {
		t.Fatal("expected write error")
	}
}

func TestEncodeContainsSections(t *testing.T) {
	data, err := Default().Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"[window]", "[git]", "[ui]"} {
		if !strings.Contains(string(data), section) {
			t.Fatalf("missing %s in %s", section, data)
		}
	}
}

func TestDefaultNetworkSettings(t *testing.T) {
	cfg := Default()
	if cfg.Git.PullStrategy != PullStrategyFF {
		t.Fatalf("PullStrategy = %q, want %q", cfg.Git.PullStrategy, PullStrategyFF)
	}
	if cfg.Git.DefaultRemote != "origin" {
		t.Fatalf("DefaultRemote = %q, want origin", cfg.Git.DefaultRemote)
	}
	if cfg.Git.PruneOnFetch {
		t.Fatal("PruneOnFetch must default to false")
	}
	if cfg.Git.ShallowDepth != 0 {
		t.Fatalf("ShallowDepth = %d, want 0", cfg.Git.ShallowDepth)
	}
}

func TestNormalizeKeepsValidPullStrategies(t *testing.T) {
	for _, strategy := range []string{PullStrategyFF, PullStrategyMerge, PullStrategyRebase} {
		cfg := Default()
		cfg.Git.PullStrategy = strategy
		cfg.Normalize()
		if cfg.Git.PullStrategy != strategy {
			t.Fatalf("PullStrategy = %q, want %q", cfg.Git.PullStrategy, strategy)
		}
	}
}

func TestNormalizeFallsBackToFFForUnknownPullStrategy(t *testing.T) {
	for _, strategy := range []string{"", "bogus", "FF"} {
		cfg := Default()
		cfg.Git.PullStrategy = strategy
		cfg.Normalize()
		if cfg.Git.PullStrategy != PullStrategyFF {
			t.Fatalf("PullStrategy(%q) = %q, want %q", strategy, cfg.Git.PullStrategy, PullStrategyFF)
		}
	}
}

func TestNormalizeFallsBackToOriginForEmptyDefaultRemote(t *testing.T) {
	cfg := Default()
	cfg.Git.DefaultRemote = ""
	cfg.Normalize()
	if cfg.Git.DefaultRemote != "origin" {
		t.Fatalf("DefaultRemote = %q, want origin", cfg.Git.DefaultRemote)
	}
}

func TestNormalizeKeepsNonEmptyDefaultRemote(t *testing.T) {
	cfg := Default()
	cfg.Git.DefaultRemote = "upstream"
	cfg.Normalize()
	if cfg.Git.DefaultRemote != "upstream" {
		t.Fatalf("DefaultRemote = %q, want upstream", cfg.Git.DefaultRemote)
	}
}

func TestNormalizeClampsNegativeShallowDepthToZero(t *testing.T) {
	cfg := Default()
	cfg.Git.ShallowDepth = -10
	cfg.Normalize()
	if cfg.Git.ShallowDepth != 0 {
		t.Fatalf("ShallowDepth = %d, want 0", cfg.Git.ShallowDepth)
	}
}

func TestNormalizeKeepsAPositiveShallowDepth(t *testing.T) {
	cfg := Default()
	cfg.Git.ShallowDepth = 50
	cfg.Normalize()
	if cfg.Git.ShallowDepth != 50 {
		t.Fatalf("ShallowDepth = %d, want 50", cfg.Git.ShallowDepth)
	}
}

func TestNormalizeKeepsPruneOnFetchTrue(t *testing.T) {
	cfg := Default()
	cfg.Git.PruneOnFetch = true
	cfg.Normalize()
	if !cfg.Git.PruneOnFetch {
		t.Fatal("PruneOnFetch must stay true")
	}
}

func TestNetworkSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.Git.PullStrategy = PullStrategyRebase
	cfg.Git.DefaultRemote = "upstream"
	cfg.Git.PruneOnFetch = true
	cfg.Git.ShallowDepth = 20
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Git.PullStrategy != PullStrategyRebase {
		t.Fatalf("PullStrategy = %q, want %q", loaded.Git.PullStrategy, PullStrategyRebase)
	}
	if loaded.Git.DefaultRemote != "upstream" {
		t.Fatalf("DefaultRemote = %q, want upstream", loaded.Git.DefaultRemote)
	}
	if !loaded.Git.PruneOnFetch {
		t.Fatal("PruneOnFetch must round trip as true")
	}
	if loaded.Git.ShallowDepth != 20 {
		t.Fatalf("ShallowDepth = %d, want 20", loaded.Git.ShallowDepth)
	}
}

func TestRepositoryOperations(t *testing.T) {
	cfg := Default()
	r := Repository{ID: "a", Name: "A", Path: "/tmp/a"}
	if !cfg.AddRepository(r) {
		t.Fatal("first add should succeed")
	}
	if cfg.AddRepository(r) {
		t.Fatal("duplicate id should fail")
	}
	if cfg.AddRepository(Repository{ID: "b", Path: "/tmp/a/"}) {
		t.Fatal("duplicate path should fail")
	}
	got, ok := cfg.FindRepository("a")
	if !ok || got.Name != "A" {
		t.Fatalf("find: %+v %v", got, ok)
	}
	if _, ok := cfg.FindRepository("zzz"); ok {
		t.Fatal("unexpected find")
	}
	if !cfg.RemoveRepository("a") {
		t.Fatal("remove should succeed")
	}
	if cfg.RemoveRepository("a") {
		t.Fatal("second remove should fail")
	}
}

func TestGroupOperations(t *testing.T) {
	cfg := Default()
	if !cfg.AddGroup(Group{ID: "g", Name: "G"}) {
		t.Fatal("add group")
	}
	if cfg.AddGroup(Group{ID: "g"}) {
		t.Fatal("duplicate group")
	}
	cfg.AddGroup(Group{ID: "child", Parent: "g"})
	cfg.AddRepository(Repository{ID: "r", Path: "/r", Group: "g"})
	if !cfg.RemoveGroup("g") {
		t.Fatal("remove group")
	}
	if cfg.RemoveGroup("g") {
		t.Fatal("second remove")
	}
	if cfg.Groups[0].Parent != "" {
		t.Fatalf("child parent not cleared: %+v", cfg.Groups)
	}
	if cfg.Repositories[0].Group != "" {
		t.Fatalf("repo group not cleared: %+v", cfg.Repositories)
	}
}

func TestTheAttributionBanIsOnUntilItIsTurnedOff(t *testing.T) {
	if !Default().Git.BanAttribution {
		t.Fatal("a new configuration must ban attribution")
	}

	kept, err := Parse([]byte("version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !kept.Git.BanAttribution {
		t.Fatal("a configuration written before the option existed must keep the ban on")
	}

	off, err := Parse([]byte("version = 1\n[git]\nban_attribution = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if off.Git.BanAttribution {
		t.Fatal("the option must be able to turn the ban off")
	}
}
