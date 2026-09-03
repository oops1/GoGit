package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/config"
)

func TestRunFailsOnBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("version = [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("expected config error")
	}
}

func TestRunFailsWithoutConfigDir(t *testing.T) {
	t.Setenv(config.EnvConfigDir, "")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if err := run(); err == nil {
		t.Fatal("expected paths error")
	}
}

func TestRunFailsOnBrokenUserI18N(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.MkdirAll(filepath.Join(dir, "i18n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "i18n", "xx.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("expected i18n error")
	}
}

func TestRunFallsBackToDiscardLoggerWhenLogFileBlocked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.MkdirAll(filepath.Join(dir, "gogit.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "i18n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "i18n", "xx.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("expected i18n error")
	}
}
