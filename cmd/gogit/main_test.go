package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

type shownMessage struct {
	title, text string
}

func captureStartupMessages(t *testing.T) *[]shownMessage {
	t.Helper()
	shown := &[]shownMessage{}
	prev := showStartupMessage
	showStartupMessage = func(title, text string) { *shown = append(*shown, shownMessage{title, text}) }
	t.Cleanup(func() { showStartupMessage = prev })
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	return shown
}

func breakUserI18N(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "i18n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "i18n", "xx.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunRecoversFromABrokenConfigAndTellsTheUser(t *testing.T) {
	shown := captureStartupMessages(t)
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	prevClock := startupClock
	startupClock = func() time.Time { return time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { startupClock = prevClock })
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("version = [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	breakUserI18N(t, dir)
	if err := run(); err == nil {
		t.Fatal("expected the i18n error that stops the app before its window")
	}
	backup := filepath.Join(dir, "config.toml.bad-20260914-080000")
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if len(*shown) != 1 || !strings.Contains((*shown)[0].text, backup) || (*shown)[0].title != "Go.Git" {
		t.Fatalf("messages = %+v", *shown)
	}
}

func TestRunFailsWhenABrokenConfigCannotBeMovedAside(t *testing.T) {
	captureStartupMessages(t)
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	prevClock := startupClock
	startupClock = func() time.Time { return time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { startupClock = prevClock })
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("version = [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "config.toml.bad-20260914-080000", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("expected config error")
	}
}

func TestRunFailsWithoutConfigDir(t *testing.T) {
	captureStartupMessages(t)
	t.Setenv(config.EnvConfigDir, "")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if err := run(); err == nil {
		t.Fatal("expected paths error")
	}
}

func TestRunFailsOnBrokenUserI18N(t *testing.T) {
	shown := captureStartupMessages(t)
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	breakUserI18N(t, dir)
	if err := run(); err == nil {
		t.Fatal("expected i18n error")
	}
	if len(*shown) != 0 {
		t.Fatalf("no message expected for a healthy config: %+v", *shown)
	}
}

func TestRunFallsBackToDiscardLoggerWhenLogFileBlocked(t *testing.T) {
	captureStartupMessages(t)
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.MkdirAll(filepath.Join(dir, "gogit.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	breakUserI18N(t, dir)
	if err := run(); err == nil {
		t.Fatal("expected i18n error")
	}
}

func TestLocalizeStartupFallsBackToBuiltinStringsWhenUserTablesAreBroken(t *testing.T) {
	captureStartupMessages(t)
	dir := t.TempDir()
	breakUserI18N(t, dir)
	localizeStartup(filepath.Join(dir, "i18n"), "ru")
	if got := widget.Tr("Startup.Failed"); got == "Startup.Failed" || !strings.Contains(got, "%s") {
		t.Fatalf("Startup.Failed = %q", got)
	}
	localizeStartup("", "en")
	if got := widget.Tr("Startup.Title"); got != "Go.Git" {
		t.Fatalf("Startup.Title = %q", got)
	}
}

func TestReportStartupFailureShowsARedactedMessage(t *testing.T) {
	shown := captureStartupMessages(t)
	reportStartupFailure(errors.New("clone https://bob:hunter2@example.com/r.git failed"))
	if len(*shown) != 1 {
		t.Fatalf("messages = %+v", *shown)
	}
	if strings.Contains((*shown)[0].text, "hunter2") || !strings.Contains((*shown)[0].text, "example.com") {
		t.Fatalf("text = %q", (*shown)[0].text)
	}
}
