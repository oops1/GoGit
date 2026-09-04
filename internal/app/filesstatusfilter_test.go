package app

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/changes"
)

func filesStatusAllowedSnapshot(a *App) map[changes.StatusFilter]bool {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	snapshot := make(map[changes.StatusFilter]bool, len(a.filesStatusAllowed))
	for k, v := range a.filesStatusAllowed {
		snapshot[k] = v
	}
	return snapshot
}

func TestRestoreFilesStatusFilterDefaultsToAllStatusesAllowed(t *testing.T) {
	a := newTestApp(t)
	allowed := filesStatusAllowedSnapshot(a)
	for _, f := range changes.AllStatusFilters {
		if !allowed[f] {
			t.Fatalf("status %v must be allowed by default", f)
		}
	}
}

func TestRestoreFilesStatusFilterAppliesConfiguredDisabledStatuses(t *testing.T) {
	cfg := config.Default()
	cfg.UI.FilesStatusFilter = []string{"ignored", "untracked", "bogus"}
	a := newTestAppWithConfig(t, cfg)
	allowed := filesStatusAllowedSnapshot(a)
	if allowed[changes.FilterIgnored] || allowed[changes.FilterUntracked] {
		t.Fatal("configured statuses must start disabled")
	}
	if !allowed[changes.FilterModified] {
		t.Fatal("statuses not in the config list must start allowed")
	}
}

func TestToggleFilesStatusFilterFlipsTheStateAndPersistsIt(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	paths := config.Paths{Dir: t.TempDir()}
	a, err := New(config.Default(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.toggleFilesStatusFilter(changes.FilterIgnored)
	if filesStatusAllowedSnapshot(a)[changes.FilterIgnored] {
		t.Fatal("toggling an allowed status must disable it")
	}
	if len(a.cfg.UI.FilesStatusFilter) != 1 || a.cfg.UI.FilesStatusFilter[0] != "ignored" {
		t.Fatalf("FilesStatusFilter = %v", a.cfg.UI.FilesStatusFilter)
	}
	if _, err := os.Stat(paths.ConfigFile()); err != nil {
		t.Fatalf("config file was not saved: %v", err)
	}

	a.toggleFilesStatusFilter(changes.FilterIgnored)
	if !filesStatusAllowedSnapshot(a)[changes.FilterIgnored] {
		t.Fatal("toggling a disabled status again must re-enable it")
	}
	if len(a.cfg.UI.FilesStatusFilter) != 0 {
		t.Fatalf("FilesStatusFilter = %v, want empty", a.cfg.UI.FilesStatusFilter)
	}
}

func TestToggleFilesStatusFilterLogsWarningWhenConfigSaveFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: dir}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.toggleFilesStatusFilter(changes.FilterModified)

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected save failure to be logged: %s", buf.String())
	}
}

func TestClickingAFilesStatusButtonTogglesItsFilter(t *testing.T) {
	a := newTestApp(t)
	btn := a.filesStatusButton(changes.FilterConflict)
	btn.OnClick()
	if filesStatusAllowedSnapshot(a)[changes.FilterConflict] {
		t.Fatal("clicking the button must disable its filter")
	}
	btn.OnClick()
	if !filesStatusAllowedSnapshot(a)[changes.FilterConflict] {
		t.Fatal("clicking the button again must re-enable its filter")
	}
}

func TestApplyFilesStatusButtonVisualsMarksDisabledButtonsDifferentlyFromEnabledOnes(t *testing.T) {
	a := newTestApp(t)
	theme := themeFor(a.EffectiveTheme())

	enabledBG := a.filesStatusButton(changes.FilterModified).Background
	a.toggleFilesStatusFilter(changes.FilterModified)
	disabledBG := a.filesStatusButton(changes.FilterModified).Background

	if enabledBG != theme.BtnPressedBG {
		t.Fatalf("enabled button background = %v, want %v", enabledBG, theme.BtnPressedBG)
	}
	if disabledBG != theme.PanelBG {
		t.Fatalf("disabled button background = %v, want %v", disabledBG, theme.PanelBG)
	}
	if enabledBG == disabledBG {
		t.Fatal("enabled and disabled buttons must look different")
	}
}

func TestApplyFilesStatusButtonVisualsSetsAStatusIconOnEveryButton(t *testing.T) {
	a := newTestApp(t)
	for _, f := range changes.AllStatusFilters {
		btn := a.filesStatusButton(f)
		if btn.Icon == nil {
			t.Fatalf("status %v: button has no icon", f)
		}
		if btn.IconPos != widget.IconOnly {
			t.Fatalf("status %v: icon position = %v, want IconOnly", f, btn.IconPos)
		}
	}
}

func TestFilesStatusButtonsHaveNonEmptyLocalizedTooltips(t *testing.T) {
	a := newTestApp(t)
	for _, f := range changes.AllStatusFilters {
		if tip := a.filesStatusButton(f).GetToolTip(); tip == "" {
			t.Fatalf("status %v: tooltip is empty", f)
		}
	}
}

func TestSetLanguageUpdatesFilesStatusButtonTooltips(t *testing.T) {
	a := newTestApp(t)
	enTip := a.filesStatusButton(changes.FilterStaged).GetToolTip()

	a.SetLanguage("ru")
	ruTip := a.filesStatusButton(changes.FilterStaged).GetToolTip()

	if ruTip == "" {
		t.Fatal("tooltip must not be empty after switching language")
	}
	if ruTip == enTip {
		t.Fatalf("tooltip did not change after switching language: %q", ruTip)
	}

	a.SetLanguage("en")
	if got := a.filesStatusButton(changes.FilterStaged).GetToolTip(); got != enTip {
		t.Fatalf("tooltip after switching back = %q, want %q", got, enTip)
	}
}
