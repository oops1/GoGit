package app

import (
	"errors"
	"os"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

func TestSetPaneVisibleClosesAndShowsPane(t *testing.T) {
	a := newTestApp(t)
	if !a.PaneVisible("journal") {
		t.Fatal("journal should start visible")
	}
	a.SetPaneVisible("journal", false)
	if a.PaneVisible("journal") {
		t.Fatal("journal should be closed")
	}
	a.SetPaneVisible("journal", true)
	if !a.PaneVisible("journal") {
		t.Fatal("journal should be visible again")
	}
	if a.PaneVisible("missing") {
		t.Fatal("unknown pane must report not visible")
	}
	a.SetPaneVisible("missing", true)
}

func TestSaveLayoutPersistsClosedPaneAcrossRestart(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	a.SetPaneVisible("journal", false)
	if err := a.SaveLayout(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.LayoutFile()); err != nil {
		t.Fatal(err)
	}
	b, err := New(config.Default(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	if b.PaneVisible("journal") {
		t.Fatal("journal must stay closed after restore")
	}
	for _, id := range []string{"repositories", "branches", "files"} {
		if !b.PaneVisible(id) {
			t.Fatalf("%s must stay visible", id)
		}
	}
}

func TestNewToleratesCorruptLayoutFile(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	paths := config.Paths{Dir: t.TempDir()}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LayoutFile(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(config.Default(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	for _, id := range []string{"repositories", "branches", "files", "journal"} {
		if !a.PaneVisible(id) {
			t.Fatalf("%s should be visible on corrupt layout", id)
		}
	}
	for side, size := range dockSideSizes {
		if got := a.Dock().SideSize(side); got != size {
			t.Fatalf("side %v size = %d, want %d", side, got, size)
		}
	}
}

func TestResetLayoutRestoresDefaultAfterClosingPanes(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	a.SetPaneVisible("journal", false)
	a.SetPaneVisible("files", false)
	if err := a.SaveLayout(); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetLayout(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"repositories", "branches", "files", "journal"} {
		if !a.PaneVisible(id) {
			t.Fatalf("%s should be visible after reset", id)
		}
	}
	for side, size := range dockSideSizes {
		if got := a.Dock().SideSize(side); got != size {
			t.Fatalf("side %v size = %d, want %d", side, got, size)
		}
	}
	if _, err := os.Stat(paths.LayoutFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("layout file should be removed: %v", err)
	}
}

func TestResetLayoutWithoutSavedFileSucceeds(t *testing.T) {
	a := newTestApp(t)
	if err := a.ResetLayout(); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchResetLayoutCommand(t *testing.T) {
	a := newTestApp(t)
	a.SetPaneVisible("journal", false)
	if !a.Dispatch(CmdResetLayout) {
		t.Fatal("reset layout command should dispatch")
	}
	if !a.PaneVisible("journal") {
		t.Fatal("journal should be visible after reset command")
	}
}

func TestRestoreLayoutReturnsLoadError(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	if err := os.MkdirAll(paths.LayoutFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.RestoreLayout(); err == nil {
		t.Fatal("expected error when layout path is a directory")
	}
}

func TestSaveLayoutFailsWhenLayoutWriteFails(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	if err := os.MkdirAll(paths.LayoutFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveLayout(); err == nil {
		t.Fatal("expected error when layout file cannot be written")
	}
}

func TestSaveLayoutFailsWhenConfigWriteFails(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	if err := os.MkdirAll(paths.ConfigFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveLayout(); err == nil {
		t.Fatal("expected error when config cannot be written")
	}
}

func TestSaveLayoutUpdatesWindowSizeAndPersistsFiles(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	a.Engine().SetResolution(1000, 700)
	if err := a.SaveLayout(); err != nil {
		t.Fatal(err)
	}
	if a.Config().Window.Width != 1000 || a.Config().Window.Height != 700 {
		t.Fatalf("window size = %dx%d, want 1000x700", a.Config().Window.Width, a.Config().Window.Height)
	}
	if _, err := os.Stat(paths.LayoutFile()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.ConfigFile()); err != nil {
		t.Fatal(err)
	}
}
