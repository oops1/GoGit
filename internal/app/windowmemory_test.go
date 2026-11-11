package app

import (
	"os"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v4/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/conflict"
)

func dialogSize(t *testing.T, a *App, size func() (int, int)) config.WindowSize {
	t.Helper()
	return readOnDispatcher(t, a, func() config.WindowSize {
		w, h := size()
		return config.WindowSize{Width: w, Height: h}
	})
}

func TestTheCompareWindowReopensAtTheSizeItWasLeft(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	captured := captureCompareView(t)
	a.openCompareFiles(textFile(t, "a.txt", "a\n"), textFile(t, "b.txt", "b\n"))
	runOnDispatcher(t, a, func() { (*captured).Dialog().Resize(1000, 600) })

	runOnDispatcher(t, a, func() { (*captured).OnClose() })
	waitForPostQueueDrain(t, a)

	want := config.WindowSize{Width: 1000, Height: 600}
	if got := readOnDispatcher(t, a, func() config.WindowSize { return a.cfg.UI.CompareWindow.WindowSize }); got != want {
		t.Fatalf("remembered = %+v, want %+v", got, want)
	}
	saved, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if saved.UI.CompareWindow.WindowSize != want {
		t.Fatalf("saved = %+v, want %+v", saved.UI.CompareWindow, want)
	}

	a.openCompareFiles("", "")
	reopened := dialogSize(t, a, func() (int, int) {
		b := (*captured).Dialog().Bounds()
		return b.Dx(), b.Dy()
	})
	if reopened != want {
		t.Fatalf("reopened at %+v, want %+v", reopened, want)
	}
	runOnDispatcher(t, a, func() { a.eng.CloseModal((*captured).Dialog()) })
}

func TestAMaximizedCompareWindowKeepsTheSizeToReturnTo(t *testing.T) {
	a := newTestApp(t)
	a.cfg.UI.CompareWindow.WindowSize = config.WindowSize{Width: 1000, Height: 600}
	captured := captureCompareView(t)
	a.openCompareFiles("", "")

	runOnDispatcher(t, a, func() { (*captured).Dialog().OnMaximizeRestore() })
	runOnDispatcher(t, a, func() { (*captured).OnClose() })
	waitForPostQueueDrain(t, a)

	want := config.WindowSize{Width: 1000, Height: 600}
	if got := readOnDispatcher(t, a, func() config.WindowSize { return a.cfg.UI.CompareWindow.WindowSize }); got != want {
		t.Fatalf("remembered = %+v, want the restored size %+v", got, want)
	}
}

func newConflictWindow(t *testing.T, a *App) *conflict.View {
	t.Helper()
	return readOnDispatcher(t, a, func() *conflict.View {
		view, err := newConflictView()
		if err != nil {
			t.Error(err)
			return nil
		}
		view.Show(conflict.File{Path: "f.txt"})
		return view
	})
}

func TestTheConflictWindowOpensTheWayItWasLeft(t *testing.T) {
	a := newTestApp(t)
	a.cfg.UI.ConflictWindow = config.ConflictWindow{
		WindowSize:     config.WindowSize{Width: 1180, Height: 700},
		HideBase:       true,
		SeparateScroll: true,
	}
	view := newConflictWindow(t, a)

	runOnDispatcher(t, a, func() { a.restoreConflictWindow(view) })

	if readOnDispatcher(t, a, view.ShowsBase) || readOnDispatcher(t, a, view.ScrollsTogether) {
		t.Fatal("the window must open with the base hidden and the sides scrolling apart")
	}
	size := dialogSize(t, a, func() (int, int) { b := view.Dialog().Bounds(); return b.Dx(), b.Dy() })
	if size != a.cfg.UI.ConflictWindow.WindowSize {
		t.Fatalf("opened at %+v, want %+v", size, a.cfg.UI.ConflictWindow.WindowSize)
	}
}

func TestTheConflictWindowRemembersItsPanesWhenClosed(t *testing.T) {
	a := newTestApp(t)
	view := newConflictWindow(t, a)
	runOnDispatcher(t, a, func() {
		a.restoreConflictWindow(view)
		a.showModal(view.Dialog(), view)
		view.SetPanes(false, false)
		view.Dialog().Resize(1200, 680)
	})

	runOnDispatcher(t, a, func() { a.eng.CloseModal(view.Dialog()) })
	waitForPostQueueDrain(t, a)

	want := config.ConflictWindow{WindowSize: config.WindowSize{Width: 1200, Height: 680}, HideBase: true, SeparateScroll: true, Split: 0.58}
	if got := readOnDispatcher(t, a, func() config.ConflictWindow { return a.cfg.UI.ConflictWindow }); got != want {
		t.Fatalf("remembered = %+v, want %+v", got, want)
	}
}

func TestAWindowSizeThatCannotBeSavedIsLogged(t *testing.T) {
	a, buf := newPanicLoggingApp(t)
	if err := os.Mkdir(a.paths.ConfigFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	view := newConflictWindowDirect(t)
	a.restoreConflictWindow(view)
	a.showModal(view.Dialog(), view)

	a.eng.CloseModal(view.Dialog())
	a.Engine().Flush()

	if !strings.Contains(buf.String(), "save window size failed") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestAnEarlierCloseListenerStillRuns(t *testing.T) {
	a := newTestApp(t)
	view := newConflictWindow(t, a)
	called := false
	runOnDispatcher(t, a, func() {
		view.Dialog().OnClosed = func() { called = true }
		a.restoreConflictWindow(view)
		a.showModal(view.Dialog(), view)
	})

	runOnDispatcher(t, a, func() { a.eng.CloseModal(view.Dialog()) })
	waitForPostQueueDrain(t, a)

	if !readOnDispatcher(t, a, func() bool { return called }) {
		t.Fatal("the listener set before the window was restored was dropped")
	}
}

func newConflictWindowDirect(t *testing.T) *conflict.View {
	t.Helper()
	view, err := newConflictView()
	if err != nil {
		t.Fatal(err)
	}
	view.Show(conflict.File{Path: "f.txt"})
	return view
}

func TestTheCompareWindowReopensWithTheSideThatWasCollapsed(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	a.openCompareFiles(textFile(t, "a.txt", "a\n"), textFile(t, "b.txt", "b\n"))
	runOnDispatcher(t, a, func() {
		(*captured).Diff().SetSplit(0.7)
		(*captured).Diff().SetCollapsed(widget.DiffLeft, true)
	})

	runOnDispatcher(t, a, func() { (*captured).OnClose() })
	waitForPostQueueDrain(t, a)

	saved := readOnDispatcher(t, a, func() config.CompareWindow { return a.cfg.UI.CompareWindow })
	if !saved.CollapseLeft || saved.CollapseRight || saved.Split != 0.7 {
		t.Fatalf("remembered = %+v, want the left side collapsed at 0.7", saved)
	}

	a.openCompareFiles("", "")
	if got := readOnDispatcher(t, a, (*captured).Layout); !got.CollapseLeft || got.Split != 0.7 {
		t.Fatalf("reopened with %+v, want the left side collapsed at 0.7", got)
	}
	runOnDispatcher(t, a, func() { a.eng.CloseModal((*captured).Dialog()) })
}

func TestTheConflictWindowRemembersItsCollapsedPanes(t *testing.T) {
	a := newTestApp(t)
	view := newConflictWindow(t, a)
	runOnDispatcher(t, a, func() {
		a.restoreConflictWindow(view)
		a.showModal(view.Dialog(), view)
		view.Merge().SetSplit(0.4)
		view.Merge().SetPaneCollapsed(widget.MergeTheirs, true)
		view.Merge().SetResultMaximized(true)
	})

	runOnDispatcher(t, a, func() { a.eng.CloseModal(view.Dialog()) })
	waitForPostQueueDrain(t, a)

	saved := readOnDispatcher(t, a, func() config.ConflictWindow { return a.cfg.UI.ConflictWindow })
	if !saved.CollapseTheirs || saved.CollapseOurs || saved.CollapseBase || !saved.ResultMaximized || saved.Split != 0.4 {
		t.Fatalf("remembered = %+v", saved)
	}

	reopened := newConflictWindow(t, a)
	runOnDispatcher(t, a, func() { a.restoreConflictWindow(reopened) })
	got := readOnDispatcher(t, a, reopened.Layout)
	if !got.CollapseTheirs || !got.ResultMaximized || got.Split != 0.4 {
		t.Fatalf("reopened with %+v", got)
	}
}

func TestAWindowClosedTwiceIsRememberedOnce(t *testing.T) {
	a := newTestApp(t)
	dlg := widget.NewDialog("Title", 400, 300)
	remembered := 0
	runOnDispatcher(t, a, func() { a.rememberOnClose(dlg, func() { remembered++ }) })

	runOnDispatcher(t, a, func() {
		dlg.OnClosed()
		dlg.OnClosed()
	})
	waitForPostQueueDrain(t, a)

	if got := readOnDispatcher(t, a, func() int { return remembered }); got != 1 {
		t.Fatalf("remembered %d times, want once", got)
	}
}
