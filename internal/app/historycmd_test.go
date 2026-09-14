package app

import (
	"context"
	"errors"
	"sync/atomic"
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

	history, found := findMenuItem(items, i18n.T("Menu.Context.FileHistory"))
	if !found || history.Disabled {
		t.Fatalf("items = %v", menuTexts(items))
	}
	if _, found := findMenuItem(items, i18n.T("Menu.Context.Blame")); !found {
		t.Fatalf("items = %v", menuTexts(items))
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

func waitForReadJobs(t *testing.T, a *App) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		a.readWG.Wait()
		close(done)
	}()
	waitForChannel(t, done, "the running reads")
	drainPostQueue(t, a)
}

func TestClosingTheRepositoryCancelsALongHistoryRead(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureHistoryViews(t)
	started := make(chan struct{})
	prev := readFileHistory
	readFileHistory = func(ctx context.Context, _ *gitrepo.Repository, _, _ string, _ ops.HistoryOptions) ([]ops.HistoryEntry, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(func() { readFileHistory = prev })

	runOnDispatcher(t, a, func() { a.openFileHistory("f.txt") })
	waitForChannel(t, started, "the history read")
	runOnDispatcher(t, a, a.CloseRepository)
	drainPostQueue(t, a)

	if n := readOnDispatcher(t, a, func() int { return len(*views) }); n != 0 {
		t.Fatalf("%d history dialogs opened for a repository that was closed", n)
	}
	if got := readOnDispatcher(t, a, a.statusLabel.Text); got == i18n.Tf("Status.FileHistoryFailed", context.Canceled) {
		t.Fatalf("status = %q, want the cancelled read kept quiet", got)
	}
}

func TestAReadStartedAfterACancelRunsNormally(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureBlameViews(t)

	runOnDispatcher(t, a, a.cancelReads)
	runOnDispatcher(t, a, func() { a.openBlame("HEAD", "f.txt") })
	view := waitForBlameView(t, a, views)

	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheSameHistoryIsNotReadTwiceAtOnce(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureHistoryViews(t)
	release := make(chan struct{})
	var calls atomic.Int32
	prev := readFileHistory
	readFileHistory = func(ctx context.Context, r *gitrepo.Repository, rev, path string, opts ops.HistoryOptions) ([]ops.HistoryEntry, error) {
		calls.Add(1)
		<-release
		return prev(ctx, r, rev, path, opts)
	}
	t.Cleanup(func() { readFileHistory = prev })

	runOnDispatcher(t, a, func() {
		a.openFileHistory("f.txt")
		a.openFileHistory("f.txt")
	})
	close(release)
	waitForReadJobs(t, a)

	if n := readOnDispatcher(t, a, func() int { return len(*views) }); calls.Load() != 1 || n != 1 {
		t.Fatalf("reads = %d, dialogs = %d, want one of each", calls.Load(), n)
	}
	runOnDispatcher(t, a, func() { (*views)[0].Dialog().CancelAction() })

	runOnDispatcher(t, a, func() { a.openFileHistory("f.txt") })
	waitForReadJobs(t, a)

	if calls.Load() != 2 {
		t.Fatalf("reads = %d, want a finished history readable again", calls.Load())
	}
	runOnDispatcher(t, a, func() { (*views)[1].Dialog().CancelAction() })
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
