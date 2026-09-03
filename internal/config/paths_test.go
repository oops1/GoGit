package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	p, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != dir {
		t.Fatalf("dir = %q, want %q", p.Dir, dir)
	}
}

func TestDefaultPathsFromUserConfigDir(t *testing.T) {
	t.Setenv(EnvConfigDir, "")
	p, err := DefaultPaths()
	if err != nil {
		t.Skip("no user config dir:", err)
	}
	base, _ := os.UserConfigDir()
	if filepath.Dir(p.Dir) != base {
		t.Fatalf("dir = %q not under %q", p.Dir, base)
	}
}

func TestDefaultPathsWithoutHome(t *testing.T) {
	t.Setenv(EnvConfigDir, "")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := DefaultPaths(); err == nil {
		t.Fatal("expected error without a user config dir")
	}
}

func TestAppDirName(t *testing.T) {
	if appDirName("windows") != "Go.Git" {
		t.Fatal("windows dir name")
	}
	if appDirName("linux") != "gogit" {
		t.Fatal("linux dir name")
	}
}

func TestPathsFiles(t *testing.T) {
	p := Paths{Dir: "root"}
	cases := map[string]string{
		p.ConfigFile():  "config.toml",
		p.LayoutFile():  "layout.json",
		p.VaultFile():   "vault.bin",
		p.LogFile():     "gogit.log",
		p.KnownHosts():  "known_hosts",
		p.UserI18NDir(): "i18n",
	}
	for full, base := range cases {
		if filepath.Base(full) != base || filepath.Dir(full) != "root" {
			t.Fatalf("%q should be root/%s", full, base)
		}
	}
}

func TestEnsureCreatesDir(t *testing.T) {
	p := Paths{Dir: filepath.Join(t.TempDir(), "a", "b")}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.Dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}
