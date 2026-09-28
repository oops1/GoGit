package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func copyGoldenConfig(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrateToCurrentIsIdentityWhenNothingIsRegistered(t *testing.T) {
	for _, from := range []int{0, 1} {
		cfg := Default()
		cfg.Language = "ru"
		migrateToCurrent(cfg, from)
		if cfg.Version != CurrentVersion || cfg.Language != "ru" {
			t.Fatalf("from %d: cfg = %+v", from, cfg)
		}
	}
}

func TestMigrateToCurrentClampsANegativeFromVersion(t *testing.T) {
	cfg := Default()
	migrateToCurrent(cfg, -5)
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
}

func TestMigrateToCurrentRunsARegisteredStepExactlyOnce(t *testing.T) {
	calls := 0
	prev, hadPrev := migrations[0]
	migrations[0] = func(cfg *Config) {
		calls++
		cfg.Language = "migrated"
	}
	t.Cleanup(func() {
		if hadPrev {
			migrations[0] = prev
		} else {
			delete(migrations, 0)
		}
	})
	cfg := Default()
	migrateToCurrent(cfg, 0)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if cfg.Language != "migrated" || cfg.Version != CurrentVersion {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestMigrateToCurrentSkipsAStepBelowFromVersion(t *testing.T) {
	calls := 0
	prev, hadPrev := migrations[0]
	migrations[0] = func(*Config) { calls++ }
	t.Cleanup(func() {
		if hadPrev {
			migrations[0] = prev
		} else {
			delete(migrations, 0)
		}
	})
	cfg := Default()
	migrateToCurrent(cfg, CurrentVersion)
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestLoadNormalizesAFileWithNoVersionField(t *testing.T) {
	cfg := loadConfig(t, copyGoldenConfig(t, "config_no_version.toml"))
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Language != "ru" || cfg.Theme != ThemeDark {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Git.DefaultRemote != "upstream" {
		t.Fatalf("git = %+v", cfg.Git)
	}
	if len(cfg.Repositories) != 1 || cfg.Repositories[0].ID != "r1" || cfg.Repositories[0].Group != "g1" {
		t.Fatalf("repositories = %+v", cfg.Repositories)
	}
	if len(cfg.Groups) != 1 || cfg.Groups[0].ID != "g1" {
		t.Fatalf("groups = %+v", cfg.Groups)
	}
}

func TestLoadNormalizesAFileWithVersionZero(t *testing.T) {
	cfg := loadConfig(t, copyGoldenConfig(t, "config_version_0.toml"))
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Language != "ru" || cfg.Theme != ThemeDark {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadKeepsAFileAlreadyAtTheCurrentVersion(t *testing.T) {
	cfg := loadConfig(t, copyGoldenConfig(t, "config_version_1.toml"))
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Language != "ru" || cfg.Theme != ThemeDark {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadKeepsUnknownKeysOfAnOlderFile(t *testing.T) {
	path := copyGoldenConfig(t, "config_unknown_keys.toml")
	cfg := loadConfig(t, path)
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	raw := decodeRaw(t, path)
	if raw["future_top_level"] != "keep me" {
		t.Fatalf("top level unknown key lost: %v", raw)
	}
	ui, _ := raw["ui"].(map[string]any)
	if ui["future_ui_flag"] != true || ui["layout"] != LayoutSidebar {
		t.Fatalf("ui = %v", ui)
	}
	section, _ := raw["future_section"].(map[string]any)
	nested, _ := section["nested"].(map[string]any)
	if section["answer"] != int64(42) || nested["deep"] != "x" {
		t.Fatalf("future section = %v", section)
	}
	repos, _ := raw["repositories"].([]map[string]any)
	if len(repos) != 1 || repos[0]["id"] != "r1" || repos[0]["future_color"] != "teal" {
		t.Fatalf("repositories = %v", repos)
	}
	again := loadConfig(t, path)
	if err := again.Save(path); err != nil {
		t.Fatal(err)
	}
	if decodeRaw(t, path)["future_top_level"] != "keep me" {
		t.Fatal("unknown keys must survive a second load/save cycle")
	}
}

func TestLoadOrRecoverBacksUpAFileFromAFutureVersion(t *testing.T) {
	path := copyGoldenConfig(t, "config_future_version.toml")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 14, 10, 11, 12, 0, time.UTC)
	cfg, backup, err := LoadOrRecover(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".bad-20260914-101112" {
		t.Fatalf("backup = %q", backup)
	}
	if cfg.Language != Default().Language {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("backup content = %q, want %q", data, original)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the future-version file must be moved away, stat err = %v", err)
	}
}

func TestLoadOrRecoverFutureVersionFailsWhenTheBackupCannotBeMade(t *testing.T) {
	path := copyGoldenConfig(t, "config_future_version.toml")
	now := time.Date(2026, 9, 14, 10, 11, 12, 0, time.UTC)
	if err := os.MkdirAll(filepath.Join(path+".bad-20260914-101112", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrRecover(path, now); err == nil {
		t.Fatal("expected a rename error")
	}
}
