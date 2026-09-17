package submodule

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func configOf(t *testing.T, text string) *config.File {
	t.Helper()
	file, err := config.Parse([]byte(text))
	if err != nil {
		t.Fatalf("config.Parse returned error %v", err)
	}
	return file
}

func TestActiveFollowsGitPrecedence(t *testing.T) {
	module := Module{Name: "lib", Path: "libs/lib"}
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"nothing configured", "", false},
		{"url registered", "[submodule \"lib\"]\n\turl = x\n", true},
		{"url without a value", "[submodule \"lib\"]\n\turl\n", false},
		{"explicit active wins over url", "[submodule \"lib\"]\n\turl = x\n\tactive = false\n", false},
		{"explicit active", "[submodule \"lib\"]\n\tactive = true\n", true},
		{"active pathspec matches", "[submodule]\n\tactive = libs/*\n", true},
		{"active pathspec misses", "[submodule]\n\tactive = other\n[submodule \"lib\"]\n\turl = x\n", false},
		{"explicit active beats pathspec", "[submodule]\n\tactive = .\n[submodule \"lib\"]\n\tactive = no\n", false},
		{"exclude pathspec", "[submodule]\n\tactive = .\n\tactive = :(exclude)libs\n", false},
	}
	for _, tt := range tests {
		got, err := Active(configOf(t, tt.text), module)
		if err != nil || got != tt.want {
			t.Fatalf("%s: Active = %v, %v; want %v", tt.name, got, err, tt.want)
		}
	}
}

func TestActiveRejectsBrokenSettings(t *testing.T) {
	module := Module{Name: "lib", Path: "lib"}
	for _, text := range []string{
		"[submodule \"lib\"]\n\tactive = maybe\n",
		"[submodule]\n\tactive = :(unknown)lib\n",
	} {
		if _, err := Active(configOf(t, text), module); !errors.Is(err, ErrInvalidActive) {
			t.Fatalf("Active(%q) returned %v", text, err)
		}
	}
}

func TestConfiguredSettingsOverrideGitmodules(t *testing.T) {
	module := Module{Name: "lib", URL: "../lib", Branch: "main", Ignore: IgnoreDirty, Update: UpdateStrategy{Type: UpdateNone}}
	empty := configOf(t, "")
	if url, ok := URL(empty, module); !ok || url != "../lib" {
		t.Fatalf("URL from .gitmodules = %q, %v", url, ok)
	}
	if url, ok := URL(empty, Module{Name: "lib"}); ok || url != "" {
		t.Fatalf("URL without any = %q, %v", url, ok)
	}
	if ignore, err := ConfiguredIgnore(empty, module); err != nil || ignore != IgnoreDirty {
		t.Fatalf("ConfiguredIgnore = %q, %v", ignore, err)
	}
	if strategy, err := Strategy(empty, module); err != nil || strategy.Type != UpdateNone {
		t.Fatalf("Strategy = %+v, %v", strategy, err)
	}
	if !SkipsUpdate(empty, module) || Branch(empty, module) != "main" {
		t.Fatal("the .gitmodules settings were not used")
	}

	cfg := configOf(t, "[submodule \"lib\"]\n\turl = https://example.com/lib\n\tbranch = dev\n\tignore = all\n\tupdate = !make\n")
	if url, ok := URL(cfg, module); !ok || url != "https://example.com/lib" {
		t.Fatalf("URL from config = %q, %v", url, ok)
	}
	if ignore, err := ConfiguredIgnore(cfg, module); err != nil || ignore != IgnoreAll {
		t.Fatalf("ConfiguredIgnore = %q, %v", ignore, err)
	}
	if strategy, err := Strategy(cfg, module); err != nil || strategy != (UpdateStrategy{Type: UpdateCommand, Command: "make"}) {
		t.Fatalf("Strategy = %+v, %v", strategy, err)
	}
	if SkipsUpdate(cfg, module) || Branch(cfg, module) != "dev" {
		t.Fatal("the config settings did not override .gitmodules")
	}
	if !SkipsUpdate(configOf(t, "[submodule \"lib\"]\n\tupdate = none\n"), Module{Name: "lib"}) {
		t.Fatal("update = none in config does not skip")
	}

	broken := configOf(t, "[submodule \"lib\"]\n\tignore = sometimes\n\tupdate = sideways\n")
	if _, err := ConfiguredIgnore(broken, module); !errors.Is(err, ErrInvalidIgnore) {
		t.Fatalf("ConfiguredIgnore returned %v", err)
	}
	if _, err := Strategy(broken, module); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("Strategy returned %v", err)
	}
}

func TestValidatePathRefusesSymbolicLinks(t *testing.T) {
	work := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(work, "link")); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(work, "real", "sub"), 0o777); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"link", "link/sub"} {
		if err := ValidatePath(work, path); !errors.Is(err, ErrSymlinkInPath) {
			t.Fatalf("ValidatePath(%q) returned %v", path, err)
		}
	}
	for _, path := range []string{"real/sub", "missing/sub"} {
		if err := ValidatePath(work, path); err != nil {
			t.Fatalf("ValidatePath(%q) returned %v", path, err)
		}
	}
}

type linkInfo struct {
	fs.FileInfo
	mode fs.FileMode
}

func (i linkInfo) Mode() fs.FileMode { return i.mode }

func TestValidatePathChecksEveryLeadingComponent(t *testing.T) {
	work := t.TempDir()
	var checked []string
	lstatPath = func(path string) (fs.FileInfo, error) {
		checked = append(checked, path)
		if filepath.Base(path) == "link" {
			return linkInfo{mode: fs.ModeSymlink}, nil
		}
		return linkInfo{mode: fs.ModeDir}, nil
	}
	defer func() { lstatPath = os.Lstat }()

	if err := ValidatePath(work, "a/b"); err != nil || len(checked) != 2 {
		t.Fatalf("ValidatePath(a/b) = %v after %v", err, checked)
	}
	if err := ValidatePath(work, "a/link/c"); !errors.Is(err, ErrSymlinkInPath) {
		t.Fatalf("ValidatePath(a/link/c) returned %v", err)
	}
}

func TestValidateGitDirRefusesADirectoryInsideAnotherGitDir(t *testing.T) {
	modules := t.TempDir()
	isGitDir := func(dir string) bool { return dir == filepath.Join(modules, "hippo") }
	if err := ValidateGitDir(modules, "hippo/hooks", isGitDir); !errors.Is(err, ErrGitDirInsideGitDir) {
		t.Fatalf("ValidateGitDir returned %v", err)
	}
	if err := ValidateGitDir(modules, "zebra/hooks", isGitDir); err != nil {
		t.Fatalf("ValidateGitDir returned %v", err)
	}
	if err := ValidateGitDir(modules, "hippo", isGitDir); err != nil {
		t.Fatalf("ValidateGitDir returned %v", err)
	}
}
