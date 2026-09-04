package app

import (
	"bytes"
	"errors"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/settings"
)

func stubShowSettings(a *App, result settings.Model, ok bool) {
	a.showSettings = func(_ settings.Model, cb func(settings.Model, bool)) {
		cb(result, ok)
	}
}

type noVisibilityWidget struct{}

func (noVisibilityWidget) Draw(widget.DrawContext)   {}
func (noVisibilityWidget) Bounds() image.Rectangle   { return image.Rectangle{} }
func (noVisibilityWidget) SetBounds(image.Rectangle) {}
func (noVisibilityWidget) Children() []widget.Widget { return nil }
func (noVisibilityWidget) AddChild(widget.Widget)    {}

func TestOpenSettingsPassesConfigDerivedModelToShowSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Language = "ru"
	cfg.Theme = config.ThemeDark
	cfg.Git.LogMaxCount = 999
	cfg.Git.AutoFetch = true
	cfg.Git.FetchInterval = 45
	cfg.Git.WorkTreeDepth = 3
	a := newTestAppWithConfig(t, cfg)

	var got settings.Model
	called := 0
	a.showSettings = func(m settings.Model, _ func(settings.Model, bool)) {
		got = m
		called++
	}

	a.Dispatch(CmdSettings)

	if called != 1 {
		t.Fatalf("showSettings called = %d, want 1", called)
	}
	want := settings.FromConfig(cfg)
	if got != want {
		t.Fatalf("model = %+v, want %+v", got, want)
	}
}

func TestApplySettingsUpdatesConfigThemeLanguageAndUISettingsAndSaves(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	newModel := settings.Model{
		Language:      "ru",
		Theme:         config.ThemeLight,
		ShowToolbar:   false,
		ShowStatusBar: false,
		LogMaxCount:   12345,
		AutoFetch:     true,
		FetchInterval: 120,
		WorkTreeDepth: 5,
	}
	stubShowSettings(a, newModel, true)

	a.Dispatch(CmdSettings)

	if a.Config().Language != "ru" || a.Config().Theme != config.ThemeLight {
		t.Fatalf("config not updated: %+v", a.Config())
	}
	if a.Config().Git.LogMaxCount != 12345 || !a.Config().Git.AutoFetch || a.Config().Git.FetchInterval != 120 {
		t.Fatalf("git config not updated: %+v", a.Config().Git)
	}
	if a.Config().Git.WorkTreeDepth != 5 {
		t.Fatalf("git worktree depth not updated: %+v", a.Config().Git)
	}
	if a.Config().UI.ShowToolbar || a.Config().UI.ShowStatusBar {
		t.Fatal("ui visibility flags not updated")
	}
	if a.Widget("toolbar").(*widget.StackPanel).IsVisible() {
		t.Fatal("toolbar widget must be hidden")
	}
	if a.Widget("statusBar").(*widget.Grid).IsVisible() {
		t.Fatal("status bar widget must be hidden")
	}
	if text, _, _ := a.MenuItemByCommand(CmdAddOrCreate); text != "Добавить или создать..." {
		t.Fatalf("menu not retranslated: %q", text)
	}
	if a.EffectiveTheme() != config.ThemeLight {
		t.Fatalf("effective theme = %q, want light", a.EffectiveTheme())
	}

	saved, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "ru" || saved.Theme != config.ThemeLight || saved.Git.LogMaxCount != 12345 || saved.Git.WorkTreeDepth != 5 {
		t.Fatalf("saved config = %+v", saved)
	}
}

func TestApplySettingsCancelLeavesConfigUnchanged(t *testing.T) {
	a := newTestApp(t)
	lang, theme, limit := a.Config().Language, a.Config().Theme, a.Config().Git.LogMaxCount
	stubShowSettings(a, settings.Model{Language: "ru", Theme: config.ThemeDark, LogMaxCount: 1}, false)

	a.Dispatch(CmdSettings)

	if a.Config().Language != lang || a.Config().Theme != theme || a.Config().Git.LogMaxCount != limit {
		t.Fatal("cancel must not change config")
	}
	if !a.Widget("toolbar").(*widget.StackPanel).IsVisible() {
		t.Fatal("cancel must not touch toolbar visibility")
	}
}

func TestApplySettingsLogsWarningWhenSaveFails(t *testing.T) {
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
	stubShowSettings(a, settings.Model{Language: "en", Theme: config.ThemeSystem, LogMaxCount: 500, FetchInterval: 300}, true)

	a.Dispatch(CmdSettings)

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected save failure to be logged: %s", buf.String())
	}
}

func TestSetToolbarAndStatusBarVisibleIgnoreWidgetsWithoutVisibilitySupport(t *testing.T) {
	a := newTestApp(t)
	a.named["toolbar"] = noVisibilityWidget{}
	a.named["statusBar"] = noVisibilityWidget{}
	a.setToolbarVisible(false)
	a.setStatusBarVisible(true)
}

func TestDefaultShowSettingsShowsModalWithoutPanicking(t *testing.T) {
	a := newTestApp(t)
	a.showSettings(settings.FromConfig(a.Config()), func(settings.Model, bool) {})
}

func TestDefaultShowSettingsLogsWarningWhenViewCreationFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	prev := newSettingsView
	wantErr := errors.New("boom")
	newSettingsView = func(widget.ModalShower, []string, settings.Model) (*settings.View, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { newSettingsView = prev })

	called := false
	a.showSettings(settings.Model{}, func(settings.Model, bool) { called = true })

	if called {
		t.Fatal("callback must not run when the dialog fails to open")
	}
	if !strings.Contains(buf.String(), "open settings dialog failed") {
		t.Fatalf("expected dialog failure to be logged: %s", buf.String())
	}
}

func TestWireSettingsViewInvokesCallbackOnSuccessfulOpen(t *testing.T) {
	a := newTestApp(t)
	view, err := settings.NewView(a.Engine(), a.languages, settings.Model{})
	if err != nil {
		t.Fatal(err)
	}

	var got settings.Model
	var gotOK bool
	a.wireSettingsView(view, func(m settings.Model, ok bool) {
		got, gotOK = m, ok
	})

	want := settings.Model{Language: "ru", Theme: config.ThemeDark, LogMaxCount: 999, FetchInterval: 60}
	view.OnOK(want)

	if !gotOK || got != want {
		t.Fatalf("result = %+v, ok = %v", got, gotOK)
	}
}

func TestWireSettingsViewReportsNotOKOnCancel(t *testing.T) {
	a := newTestApp(t)
	view, err := settings.NewView(a.Engine(), a.languages, settings.Model{})
	if err != nil {
		t.Fatal(err)
	}

	gotOK := true
	a.wireSettingsView(view, func(_ settings.Model, ok bool) { gotOK = ok })

	view.OnCancel()

	if gotOK {
		t.Fatal("cancel must report ok = false")
	}
}
