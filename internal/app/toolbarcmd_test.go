package app

import (
	"bytes"
	"cmp"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs/toolbar"
)

func captureToolbarViews(t *testing.T) *[]*toolbar.View {
	t.Helper()
	views := &[]*toolbar.View{}
	prev := newToolbarView
	newToolbarView = func(m *toolbar.Model) (*toolbar.View, error) {
		view, err := prev(m)
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newToolbarView = prev })
	return views
}

func TestTheCatalogOffersTheSpacersFirstAndTheCommandsByName(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	entries := toolbarCatalogEntries()
	if entries[0].ID != toolbar.SeparatorID || entries[1].ID != toolbar.StretchID {
		t.Fatalf("the list opens with %q and %q", entries[0].ID, entries[1].ID)
	}
	if len(entries) != len(toolbarCatalog())+2 {
		t.Fatalf("catalog entries = %d, commands = %d", len(entries), len(toolbarCatalog()))
	}
	commands := entries[2:]
	sorted := slices.IsSortedFunc(commands, func(a, b toolbar.Entry) int {
		return cmp.Or(cmp.Compare(a.Label, b.Label), cmp.Compare(a.ID, b.ID))
	})
	if !sorted {
		t.Fatal("the commands must be listed by their caption")
	}
	for _, entry := range commands {
		if entry.Label == "" {
			t.Fatalf("catalog entry %q has no caption", entry.ID)
		}
	}
}

func TestConfiguringTheToolbarRebuildsItAndSurvivesARestart(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	views := captureToolbarViews(t)

	if !readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdConfigureToolbar) }) {
		t.Fatal("configure toolbar must always be available")
	}
	if len(*views) != 1 {
		t.Fatalf("views = %d", len(*views))
	}
	view := (*views)[0]
	row := []string{string(CmdCommit), toolbar.StretchID, string(CmdRefresh)}
	runOnDispatcher(t, a, func() { view.OnOK(toolbar.Result{Items: row, Captions: false}) })

	if got := toolbarItemIDs(a); !slices.Equal(got, row) {
		t.Fatalf("toolbar = %v, want %v", got, row)
	}
	if a.cfg.UI.ToolbarCaptions {
		t.Fatal("the captions flag must follow the dialog")
	}

	back, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(back.UI.ToolbarItems, row) || back.UI.ToolbarCaptions {
		t.Fatalf("saved config = %v, captions %v", back.UI.ToolbarItems, back.UI.ToolbarCaptions)
	}
	restarted := newTestAppWithConfig(t, back)
	if got := toolbarItemIDs(restarted); !slices.Equal(got, row) {
		t.Fatalf("after a restart toolbar = %v, want %v", got, row)
	}
}

func TestCancellingTheDialogLeavesTheToolbarAlone(t *testing.T) {
	a := newTestApp(t)
	views := captureToolbarViews(t)
	runOnDispatcher(t, a, func() { a.Dispatch(CmdConfigureToolbar) })
	runOnDispatcher(t, a, (*views)[0].OnCancel)
	if got := toolbarItemIDs(a); !slices.Equal(got, defaultToolbarItems()) {
		t.Fatalf("toolbar = %v", got)
	}
}

func TestAFailingDialogIsOnlyLogged(t *testing.T) {
	a := newTestApp(t)
	prev := newToolbarView
	newToolbarView = func(*toolbar.Model) (*toolbar.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newToolbarView = prev })
	runOnDispatcher(t, a, func() { a.Dispatch(CmdConfigureToolbar) })
	if got := toolbarItemIDs(a); !slices.Equal(got, defaultToolbarItems()) {
		t.Fatalf("toolbar = %v", got)
	}
}

func TestConfiguringTheToolbarLogsAFailedSave(t *testing.T) {
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

	a.applyToolbarConfiguration(toolbar.Result{Items: []string{string(CmdCommit)}, Captions: true})

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected the failed save to be logged: %s", buf.String())
	}
}

func TestTheDialogStartsFromTheConfiguredRow(t *testing.T) {
	cfg := config.Default()
	cfg.UI.ToolbarItems = []string{string(CmdCommit), toolbar.SeparatorID}
	a := newTestAppWithConfig(t, cfg)
	if got := a.toolbarModel().Items(); !slices.Equal(got, cfg.UI.ToolbarItems) {
		t.Fatalf("model row = %v", got)
	}
	if got := a.toolbarModel().Defaults(); !slices.Equal(got, defaultToolbarItems()) {
		t.Fatalf("model defaults = %v", got)
	}
}
