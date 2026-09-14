package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/remote"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/ui/operation"
)

func openedBusyApp(t *testing.T) *App {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)
	return a
}

func blockingWrite(started, release chan struct{}) writeFunc {
	return func(context.Context, *gitrepo.Repository) error {
		close(started)
		<-release
		return nil
	}
}

func waitForChannel(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(testTimeout):
		t.Fatalf("%s did not happen in time", what)
	}
}

func TestAWriteIsRefusedWhileAnOperationRuns(t *testing.T) {
	a := openedBusyApp(t)
	views := captureOperationViews(t)
	started := make(chan struct{})
	release := make(chan struct{})
	a.RunOperation("Pull", func(context.Context, OperationReporter) error {
		close(started)
		<-release
		return nil
	})
	waitForChannel(t, started, "the operation start")

	refused := !a.startWrite(func(context.Context, *gitrepo.Repository) error {
		t.Error("a write must not run next to an operation")
		return nil
	}, func(error) {})

	if !refused {
		t.Fatal("startWrite must refuse while an operation runs")
	}
	waitForStatusText(t, a, i18n.T("Status.Busy"))
	close(release)
	waitForFinishedOperation(t, a, lastOperationView(t, views))
	done := make(chan struct{})
	if !a.startWrite(func(context.Context, *gitrepo.Repository) error { return nil }, func(error) { close(done) }) {
		t.Fatal("a write must start once the operation has finished")
	}
	waitForChannel(t, done, "the write after the operation")
}

func TestAnOperationIsRefusedWhileAWriteRuns(t *testing.T) {
	a := openedBusyApp(t)
	views := captureOperationViews(t)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	a.startWrite(blockingWrite(started, release), func(error) { close(done) })
	waitForChannel(t, started, "the write start")

	a.RunOperation("Pull", func(context.Context, OperationReporter) error {
		t.Error("an operation must not run next to a write")
		return nil
	})

	if len(*views) != 0 {
		t.Fatal("a refused operation must not open its window")
	}
	waitForStatusText(t, a, i18n.T("Status.Busy"))
	close(release)
	waitForChannel(t, done, "the write end")
}

func TestASecondOperationIsRefusedWhileTheFirstRuns(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	started := make(chan struct{})
	release := make(chan struct{})
	a.RunOperation("Fetch", func(context.Context, OperationReporter) error {
		close(started)
		<-release
		return nil
	})
	waitForChannel(t, started, "the first operation start")

	a.RunOperation("Push", func(context.Context, OperationReporter) error {
		t.Error("a second operation must not run next to the first")
		return nil
	})

	if len(*views) != 1 {
		t.Fatalf("windows = %d, want only the first operation", len(*views))
	}
	close(release)
	waitForFinishedOperation(t, a, lastOperationView(t, views))
}

func TestAnOperationWindowThatCannotOpenLeavesTheAppFree(t *testing.T) {
	a := newTestApp(t)
	prev := newOperationView
	newOperationView = func(widget.ModalShower, string) (*operation.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newOperationView = prev })

	a.RunOperation("Fetch", func(context.Context, OperationReporter) error { return nil })

	if a.busy() {
		t.Fatal("an operation that never opened must not keep the app busy")
	}
	a.stopNetOperations()
}

func TestRunOperationHoldsTheWatcherAroundTheBody(t *testing.T) {
	target := filepath.Join(t.TempDir(), "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }
	a.ActivateRepository("r1")
	<-fw.started
	views := captureOperationViews(t)
	var pausedInside, resumedInside int32

	a.RunOperation("Pull", func(context.Context, OperationReporter) error {
		pausedInside = fw.pauses.Load()
		resumedInside = fw.resumes.Load()
		return nil
	})
	waitForFinishedOperation(t, a, lastOperationView(t, views))

	if pausedInside != 1 || resumedInside != 0 {
		t.Fatalf("inside the body pauses=%d resumes=%d, want the watcher paused", pausedInside, resumedInside)
	}
	if fw.resumes.Load() != 1 || fw.pokes.Load() != 1 {
		t.Fatalf("after the body resumes=%d pokes=%d, want 1 each", fw.resumes.Load(), fw.pokes.Load())
	}
}

func TestAutoFetchSkipsWhileAWriteRuns(t *testing.T) {
	a := openedBusyApp(t)
	var fetches atomic.Int32
	prev := autoFetchOpsFetch
	autoFetchOpsFetch = func(context.Context, *gitrepo.Repository, string, remote.FetchOptions) (remote.FetchResult, error) {
		fetches.Add(1)
		return remote.FetchResult{}, nil
	}
	t.Cleanup(func() { autoFetchOpsFetch = prev })
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	a.startWrite(blockingWrite(started, release), func(error) { close(done) })
	waitForChannel(t, started, "the write start")

	a.performAutoFetch(t.Context())

	close(release)
	waitForChannel(t, done, "the write end")
	if got := fetches.Load(); got != 0 {
		t.Fatalf("fetches = %d, want none while a write runs", got)
	}
}
