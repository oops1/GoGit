package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

const futureConfig = `version = 1
language = "ru"
future_top = "keep me"

[ui]
layout = "sidebar"
future_flag = true

[future_section]
answer = 42
nested = { deep = "x" }

[[groups]]
id = "g1"
name = "Work"

[[repositories]]
id = "r1"
name = "one"
path = "/src/one"
color = "red"

[[repositories]]
id = "r2"
name = "two"
path = "/src/two"
color = "blue"

[[future_list]]
name = "a"
`

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadConfig(t *testing.T, path string) *Config {
	t.Helper()
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func decodeRaw(t *testing.T, path string) map[string]any {
	t.Helper()
	var raw map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	return raw
}

func repositoryIDs(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Repositories))
	for _, r := range cfg.Repositories {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

func TestSaveKeepsSettingsUnknownToThisBuild(t *testing.T) {
	path := writeConfigFile(t, futureConfig)
	cfg := loadConfig(t, path)
	cfg.Theme = ThemeDark
	cfg.RemoveRepository("r2")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	raw := decodeRaw(t, path)
	if raw["future_top"] != "keep me" || raw["theme"] != ThemeDark {
		t.Fatalf("top level = %v", raw)
	}
	ui, _ := raw["ui"].(map[string]any)
	if ui["future_flag"] != true || ui["layout"] != LayoutSidebar {
		t.Fatalf("ui = %v", ui)
	}
	section, _ := raw["future_section"].(map[string]any)
	nested, _ := section["nested"].(map[string]any)
	if section["answer"] != int64(42) || nested["deep"] != "x" {
		t.Fatalf("future section = %v", section)
	}
	repos, _ := raw["repositories"].([]map[string]any)
	if len(repos) != 1 || repos[0]["id"] != "r1" || repos[0]["color"] != "red" {
		t.Fatalf("repositories = %v", repos)
	}
	list, _ := raw["future_list"].([]map[string]any)
	if len(list) != 1 || list[0]["name"] != "a" {
		t.Fatalf("future list = %v", list)
	}
	again := loadConfig(t, path)
	if err := again.Save(path); err != nil {
		t.Fatal(err)
	}
	if decodeRaw(t, path)["future_top"] != "keep me" {
		t.Fatal("unknown keys must survive repeated saves")
	}
}

func TestSaveWithoutUnknownKeysKeepsTheStructOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Default().Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "version = 1") {
		t.Fatalf("unexpected order:\n%s", data)
	}
}

func TestTwoConfigsOnOnePathKeepEachOthersRepositories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	first := loadConfig(t, path)
	second := loadConfig(t, path)
	first.AddRepository(Repository{ID: "a", Path: "/src/a"})
	if err := first.Save(path); err != nil {
		t.Fatal(err)
	}
	second.AddRepository(Repository{ID: "b", Path: "/src/b"})
	second.AddGroup(Group{ID: "g", Name: "G"})
	if err := second.Save(path); err != nil {
		t.Fatal(err)
	}
	first.Theme = ThemeLight
	if err := first.Save(path); err != nil {
		t.Fatal(err)
	}
	final := loadConfig(t, path)
	if got := repositoryIDs(final); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("repositories = %v", got)
	}
	if len(final.Groups) != 1 || final.Theme != ThemeLight {
		t.Fatalf("groups = %v, theme = %q", final.Groups, final.Theme)
	}
}

func TestConfigKeepsRemovalsMadeByAnotherInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	seed := Default()
	seed.AddRepository(Repository{ID: "old", Path: "/src/old"})
	seed.AddGroup(Group{ID: "g", Name: "G"})
	if err := seed.Save(path); err != nil {
		t.Fatal(err)
	}
	first := loadConfig(t, path)
	second := loadConfig(t, path)
	first.RemoveRepository("old")
	first.RemoveGroup("g")
	if err := first.Save(path); err != nil {
		t.Fatal(err)
	}
	second.AddRepository(Repository{ID: "new", Path: "/src/new"})
	if err := second.Save(path); err != nil {
		t.Fatal(err)
	}
	final := loadConfig(t, path)
	if got := repositoryIDs(final); !slices.Equal(got, []string{"new"}) {
		t.Fatalf("repositories = %v", got)
	}
	if len(final.Groups) != 0 {
		t.Fatalf("groups = %v", final.Groups)
	}
}

func TestMergeDropsARepositoryAddedTwiceUnderDifferentIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	first := loadConfig(t, path)
	second := loadConfig(t, path)
	first.AddRepository(Repository{ID: "a", Path: "/src/same"})
	if err := first.Save(path); err != nil {
		t.Fatal(err)
	}
	second.AddRepository(Repository{ID: "b", Path: "/src/same/"})
	if err := second.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := repositoryIDs(loadConfig(t, path)); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("repositories = %v", got)
	}
}

func TestMergeKeepsTheHighestVaultGenerationAndNewUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	first := loadConfig(t, path)
	second := loadConfig(t, path)
	first.Security.VaultGeneration = 9
	if err := first.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := first.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "config.toml"), append([]byte("from_newer_build = 1\n"), data...), 0o600); err != nil {
		t.Fatal(err)
	}
	second.Security.VaultGeneration = 3
	if err := second.Save(path); err != nil {
		t.Fatal(err)
	}
	final := loadConfig(t, path)
	if final.Security.VaultGeneration != 9 {
		t.Fatalf("vault generation = %d, want 9", final.Security.VaultGeneration)
	}
	if decodeRaw(t, path)["from_newer_build"] != int64(1) {
		t.Fatal("keys written by a newer build in the meantime must survive")
	}
}

func TestSaveRefusesToOverwriteAConfigFromANewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := loadConfig(t, path)
	if err := os.WriteFile(path, []byte("version = 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestSaveReplacesABrokenFileWrittenInTheMeantime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := loadConfig(t, path)
	cfg.Language = "ru"
	if err := os.WriteFile(path, []byte("version = [broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if loadConfig(t, path).Language != "ru" {
		t.Fatal("the broken file must be replaced")
	}
}

func TestLoadOrRecoverBacksUpABrokenFileAndStartsWithDefaults(t *testing.T) {
	path := writeConfigFile(t, "version = [broken")
	now := time.Date(2026, 9, 14, 10, 11, 12, 0, time.UTC)
	cfg, backup, err := LoadOrRecover(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".bad-20260914-101112" {
		t.Fatalf("backup = %q", backup)
	}
	if cfg.Language != Default().Language {
		t.Fatalf("cfg = %+v", cfg)
	}
	data, err := os.ReadFile(backup)
	if err != nil || string(data) != "version = [broken" {
		t.Fatalf("backup content = %q, %v", data, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the broken file must be moved away, stat err = %v", err)
	}
}

func TestLoadOrRecoverFailsWhenTheBackupCannotBeMade(t *testing.T) {
	path := writeConfigFile(t, "version = [broken")
	now := time.Date(2026, 9, 14, 10, 11, 12, 0, time.UTC)
	if err := os.MkdirAll(filepath.Join(path+".bad-20260914-101112", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrRecover(path, now); err == nil {
		t.Fatal("expected a rename error")
	}
}

func TestLoadOrRecoverPassesOtherResultsThrough(t *testing.T) {
	path := writeConfigFile(t, "version = 99")
	if _, backup, err := LoadOrRecover(path, time.Now()); !errors.Is(err, ErrUnsupportedVersion) || backup != "" {
		t.Fatalf("backup = %q, err = %v", backup, err)
	}
	valid := writeConfigFile(t, `language = "ru"`)
	cfg, backup, err := LoadOrRecover(valid, time.Now())
	if err != nil || backup != "" || cfg.Language != "ru" {
		t.Fatalf("cfg = %+v, backup = %q, err = %v", cfg, backup, err)
	}
}

func TestParseReportsSyntaxErrorsAsInvalid(t *testing.T) {
	if _, err := Parse([]byte("language = [broken")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestCollectAtIgnoresPathsThatAreNotInTheDocument(t *testing.T) {
	raw := map[string]any{"scalar": 1, "table": map[string]any{}}
	for _, key := range [][]string{{"missing"}, {"scalar", "child"}, {"table", "missing"}} {
		if got := collectAt(raw, key, nil); got != nil {
			t.Fatalf("key %v: got %v", key, got)
		}
	}
}

func TestCollectExtrasUsesTheIndexForElementsWithoutAnID(t *testing.T) {
	raw := map[string]any{"list": []map[string]any{{"x": 1}, {"x": 2}}}
	extras := collectExtras(raw, []toml.Key{{"list", "x"}, {"list", "x"}})
	if len(extras) != 2 || extras[0].path[0].element != "#0" || extras[1].path[0].element != "#1" {
		t.Fatalf("extras = %+v", extras)
	}
}

func TestMergeExtrasPrefersTheFileAndKeepsOwnUnknownKeys(t *testing.T) {
	extra := func(name string, value any) extraValue {
		return extraValue{path: []pathStep{{name: name}}, value: value}
	}
	merged := mergeExtras([]extraValue{extra("a", 1), extra("b", 1)}, []extraValue{extra("b", 2), extra("c", 2)})
	got := map[string]any{}
	for _, e := range merged {
		got[e.path[0].name] = e.value
	}
	if len(merged) != 3 || got["a"] != 1 || got["b"] != 2 || got["c"] != 2 {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestInsertExtraSkipsConflictsAndMissingElements(t *testing.T) {
	doc := map[string]any{
		"scalar": "known",
		"list":   []map[string]any{{"id": "a"}},
	}
	insertExtra(doc, []pathStep{{name: "scalar"}, {name: "child"}}, 1)
	insertExtra(doc, []pathStep{{name: "list", element: "id=missing"}, {name: "x"}}, 1)
	insertExtra(doc, []pathStep{{name: "list", element: "id=a"}, {name: "x"}}, 2)
	insertExtra(doc, []pathStep{{name: "fresh"}, {name: "y"}}, 3)
	insertExtra(doc, []pathStep{{name: "scalar"}}, "extra")
	if doc["scalar"] != "known" {
		t.Fatalf("known values must win: %v", doc["scalar"])
	}
	list, _ := doc["list"].([]map[string]any)
	if list[0]["x"] != 2 || len(list) != 1 {
		t.Fatalf("list = %v", list)
	}
	fresh, _ := doc["fresh"].(map[string]any)
	if fresh["y"] != 3 {
		t.Fatalf("fresh = %v", doc["fresh"])
	}
}
