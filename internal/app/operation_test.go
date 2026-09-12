package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/operation"
)

func captureOperationViews(t *testing.T) *[]*operation.View {
	t.Helper()
	views := &[]*operation.View{}
	prev := newOperationView
	newOperationView = func(eng widget.ModalShower, title string) (*operation.View, error) {
		view, err := prev(eng, title)
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newOperationView = prev })
	return views
}

func lastOperationView(t *testing.T, views *[]*operation.View) *operation.View {
	t.Helper()
	if len(*views) == 0 {
		t.Fatal("no operation window was opened")
	}
	return (*views)[len(*views)-1]
}

func readOnDispatcher[T any](t *testing.T, a *App, read func() T) T {
	t.Helper()
	ch := make(chan T, 1)
	a.Post(func() { ch <- read() })
	select {
	case value := <-ch:
		return value
	case <-time.After(testTimeout):
		t.Fatal("dispatcher did not answer in time")
		var zero T
		return zero
	}
}

func waitForFinishedOperation(t *testing.T, a *App, view *operation.View) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, view.Running) {
		if time.Now().After(deadline) {
			t.Fatal("operation did not finish in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunOperationReportsProgressAndFinishes(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		reporter.Status("scanning")
		reporter.Log("first")
		reporter.Progress(0.5)
		return nil
	})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)
	if !slices.Contains(lines, "first") {
		t.Fatalf("log = %v", lines)
	}
}

func TestRunOperationCancelStopsTheBody(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	started := make(chan struct{})

	a.RunOperation("Fetch", func(ctx context.Context, _ OperationReporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	view := lastOperationView(t, views)
	<-started
	readOnDispatcher(t, a, func() bool { view.OnCancel(); return true })
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)
	if len(lines) == 0 {
		t.Fatal("cancelled operation left no trace in the log")
	}
}

func TestEscapeCancelsTheRunningOperation(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	started := make(chan struct{})

	a.RunOperation("Fetch", func(ctx context.Context, _ OperationReporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	view := lastOperationView(t, views)
	<-started
	readOnDispatcher(t, a, func() bool { view.Dialog().OnCancel(); return true })
	waitForFinishedOperation(t, a, view)
}

func TestRunOperationCloseClosesTheModal(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	a.RunOperation("Fetch", func(context.Context, OperationReporter) error { return nil })

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	readOnDispatcher(t, a, func() bool { view.OnClose(); return true })
}

func TestRunOperationKeepsQuietWhenTheWindowCannotOpen(t *testing.T) {
	a := newTestApp(t)
	prev := newOperationView
	wantErr := errors.New("boom")
	newOperationView = func(widget.ModalShower, string) (*operation.View, error) { return nil, wantErr }
	t.Cleanup(func() { newOperationView = prev })

	called := false
	a.RunOperation("Fetch", func(context.Context, OperationReporter) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("the operation body must not run without its window")
	}
}

func TestOperationProgressLogsEveryCorePhaseTranslated(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	phases := []string{
		"init",
		"connecting",
		"negotiating",
		progress.PhaseReceiving,
		progress.PhaseCounting,
		progress.PhaseCompressing,
		progress.PhaseWriting,
		progress.PhaseResolving,
		"updating-refs",
		progress.PhaseCheckout,
	}
	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		for _, phase := range phases {
			prog.Phase(phase)
		}
		return nil
	})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)

	want := []string{
		i18n.T("Operation.Log.Initializing"),
		i18n.T("Operation.Log.Connecting"),
		i18n.T("Operation.Log.Negotiating"),
		i18n.T("Operation.Log.Receiving"),
		i18n.T("Operation.Log.Counting"),
		i18n.T("Operation.Log.Compressing"),
		i18n.T("Operation.Log.Writing"),
		i18n.T("Operation.Log.Resolving"),
		i18n.T("Operation.Log.Updating"),
		i18n.T("Operation.Log.Checkout"),
	}
	for _, w := range want {
		if !slices.Contains(lines, w) {
			t.Fatalf("log = %v, missing phase line %q", lines, w)
		}
	}
}

func TestOperationProgressCountsTheObjectsOnOneLinePerPhase(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	a.RunOperation("Push", func(_ context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		prog.Phase(progress.PhaseCounting)
		for i := int64(1); i <= 3; i++ {
			prog.Count(progress.PhaseCounting, i, 0)
		}
		for i := int64(1); i <= 3; i++ {
			prog.Count(progress.PhaseCompressing, i, 3)
			prog.Count(progress.PhaseWriting, i, 3)
		}
		return nil
	})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)

	want := []string{
		i18n.Tf("Operation.Log.CountingObjects", int64(3)),
		i18n.Tf("Operation.Log.CompressingObjects", int64(3), int64(3)),
		i18n.Tf("Operation.Log.WritingObjects", int64(3), int64(3)),
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
}

func TestOperationProgressKeepsOneLineForAPhaseThatCannotCount(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		prog.Phase(progress.PhaseReceiving)
		prog.Count(progress.PhaseReceiving, 4096, 0)
		prog.Count(progress.PhaseReceiving, 8192, 0)
		prog.Message("remote: well done")
		return nil
	})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)

	want := []string{i18n.T("Operation.Log.Receiving"), "remote: well done"}
	if !slices.Equal(lines, want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
}

func TestASecondOperationStartsItsOwnProgressLines(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	a.RunOperation("Sync", func(_ context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		for range 2 {
			prog.Phase("connecting")
			prog.Count(progress.PhaseWriting, 1, 1)
		}
		return nil
	})

	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)

	connecting := i18n.T("Operation.Log.Connecting")
	writing := i18n.Tf("Operation.Log.WritingObjects", int64(1), int64(1))
	if !slices.Equal(lines, []string{connecting, writing, connecting, writing}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestStopNetOperationsCancelsARunningOperationAndWaits(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	started := make(chan struct{})

	a.RunOperation("Fetch", func(ctx context.Context, _ OperationReporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started

	a.stopNetOperations()

	view := lastOperationView(t, views)
	if readOnDispatcher(t, a, view.Running) {
		t.Fatal("stopping net operations must cancel the running one")
	}
	a.stopNetOperations()
}
