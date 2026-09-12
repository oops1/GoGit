package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/blame"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/blameview"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/filehistory"
)

func captureHistoryViews(t *testing.T) *[]*filehistory.View {
	t.Helper()
	views := &[]*filehistory.View{}
	prev := newFileHistoryView
	newFileHistoryView = func() (*filehistory.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newFileHistoryView = prev })
	return views
}

func captureBlameViews(t *testing.T) *[]*blameview.View {
	t.Helper()
	views := &[]*blameview.View{}
	prev := newBlameView
	newBlameView = func() (*blameview.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newBlameView = prev })
	return views
}

func waitForDialog(t *testing.T, a *App, count func() int, what string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		waitForPostQueueDrain(t, a)
		if readOnDispatcher(t, a, count) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not open", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForHistoryView(t *testing.T, a *App, views *[]*filehistory.View) *filehistory.View {
	t.Helper()
	waitForDialog(t, a, func() int { return len(*views) }, "the file history dialog")
	return (*views)[len(*views)-1]
}

func waitForBlameView(t *testing.T, a *App, views *[]*blameview.View) *blameview.View {
	t.Helper()
	waitForDialog(t, a, func() int { return len(*views) }, "the blame dialog")
	return (*views)[len(*views)-1]
}

func TestTheFilesMenuOffersTheHistoryAndTheBlame(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForWorkingRows(t, a, 0)

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.filesMenu(changes.Row{RelPath: "f.txt"}, 0) })

	last := items[len(items)-2]
	if last.Text != i18n.T("Menu.Context.FileHistory") || last.Disabled {
		t.Fatalf("items = %+v", items)
	}
	if items[len(items)-1].Text != i18n.T("Menu.Context.Blame") {
		t.Fatalf("items = %+v", items)
	}
}

func TestTheFilesMenuSkipsTheHistoryWithoutAPath(t *testing.T) {
	a, _ := forkedApp(t, false)

	if items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.historyItems("") }); items != nil {
		t.Fatalf("items = %+v", items)
	}
}

func TestTheHistoryDialogListsTheCommitsOfTheFile(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureHistoryViews(t)

	readOnDispatcher(t, a, func() bool { a.openFileHistory("f.txt"); return true })
	view := waitForHistoryView(t, a, views)

	entries := readOnDispatcher(t, a, view.Entries)
	if len(entries) == 0 || entries[0].Subject == "" {
		t.Fatalf("entries = %+v", entries)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheHistoryDialogOpensTheBlame(t *testing.T) {
	a, _ := forkedApp(t, false)
	historyViews := captureHistoryViews(t)
	blameViews := captureBlameViews(t)

	readOnDispatcher(t, a, func() bool { a.openFileHistory("f.txt"); return true })
	view := waitForHistoryView(t, a, historyViews)
	entries := readOnDispatcher(t, a, view.Entries)
	readOnDispatcher(t, a, func() bool { view.OnBlame(entries[0]); return true })
	blamed := waitForBlameView(t, a, blameViews)

	lines := readOnDispatcher(t, a, blamed.Lines)
	if len(lines) == 0 || lines[0].Commit.IsZero() {
		t.Fatalf("lines = %+v", lines)
	}
	readOnDispatcher(t, a, func() bool { blamed.Dialog().CancelAction(); return true })
}

func TestTheBlameDialogOpensFromTheFilesMenu(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureBlameViews(t)

	readOnDispatcher(t, a, func() bool { a.openBlame("HEAD", "f.txt"); return true })
	view := waitForBlameView(t, a, views)

	if lines := readOnDispatcher(t, a, view.Lines); len(lines) == 0 {
		t.Fatal("the blame has no lines")
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestAFailedHistoryOrBlameIsReported(t *testing.T) {
	a, _ := forkedApp(t, false)
	prevHistory, prevBlame := readFileHistory, readBlame
	readFileHistory = func(context.Context, *gitrepo.Repository, string, string, ops.HistoryOptions) ([]ops.HistoryEntry, error) {
		return nil, errors.New("no history")
	}
	readBlame = func(context.Context, *gitrepo.Repository, string, string, ops.BlameOptions) (blame.Result, error) {
		return blame.Result{}, errors.New("no blame")
	}
	t.Cleanup(func() { readFileHistory, readBlame = prevHistory, prevBlame })

	readOnDispatcher(t, a, func() bool { a.openFileHistory("f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.FileHistoryFailed", errors.New("no history")))

	readOnDispatcher(t, a, func() bool { a.openBlame("HEAD", "f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.BlameFailed", errors.New("no blame")))
}

func TestTheHistoryAndBlameNeedARepository(t *testing.T) {
	a := newTestApp(t)
	historyViews := captureHistoryViews(t)
	blameViews := captureBlameViews(t)

	a.openFileHistory("f.txt")
	a.openBlame("HEAD", "f.txt")
	items := a.historyItems("f.txt")

	if len(*historyViews) != 0 || len(*blameViews) != 0 || !items[1].Disabled {
		t.Fatal("the dialogs opened without a repository")
	}
}

func TestTheHistoryAndBlameDialogFailuresAreLogged(t *testing.T) {
	a, _ := forkedApp(t, false)
	prevHistory, prevBlame := newFileHistoryView, newBlameView
	newFileHistoryView = func() (*filehistory.View, error) { return nil, errors.New("no dialog") }
	newBlameView = func() (*blameview.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newFileHistoryView, newBlameView = prevHistory, prevBlame })

	readOnDispatcher(t, a, func() bool { a.openFileHistory("f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.FileHistoryFailed", errors.New("no dialog")))

	readOnDispatcher(t, a, func() bool { a.openBlame("HEAD", "f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.BlameFailed", errors.New("no dialog")))
}

func TestAReadJobNeedsARepository(t *testing.T) {
	a := newTestApp(t)

	if a.startRead(func(context.Context, *gitrepo.Repository) error { return nil }, func(error) {}) {
		t.Fatal("a read job started without a repository")
	}
}

func TestTheFilesMenuItemsOpenTheDialogs(t *testing.T) {
	a, _ := forkedApp(t, false)
	historyViews := captureHistoryViews(t)
	blameViews := captureBlameViews(t)
	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.historyItems("f.txt") })

	readOnDispatcher(t, a, func() bool { items[1].OnClick(); return true })
	history := waitForHistoryView(t, a, historyViews)
	readOnDispatcher(t, a, func() bool { history.Dialog().CancelAction(); return true })

	readOnDispatcher(t, a, func() bool { items[2].OnClick(); return true })
	blamed := waitForBlameView(t, a, blameViews)
	readOnDispatcher(t, a, func() bool { blamed.Dialog().CancelAction(); return true })
}
