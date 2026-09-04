package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

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

func TestRemoteCommandsOpenTheOperationWindow(t *testing.T) {
	for _, tc := range []struct {
		command CommandID
		key     string
	}{
		{CmdPull, "Operation.Title.Pull"},
		{CmdSync, "Operation.Title.Sync"},
		{CmdPush, "Operation.Title.Push"},
	} {
		t.Run(string(tc.command), func(t *testing.T) {
			a := newTestApp(t)
			a.SetActiveRepository("r", false)
			views := captureOperationViews(t)

			if !a.Dispatch(tc.command) {
				t.Fatalf("%s was not dispatched", tc.command)
			}

			view := lastOperationView(t, views)
			waitForFinishedOperation(t, a, view)
			lines := readOnDispatcher(t, a, view.Lines)
			joined := strings.Join(lines, "\n")
			if !strings.Contains(joined, i18n.T("Operation.Log.RemoteUnavailable")) {
				t.Fatalf("log = %v", lines)
			}
			if !strings.Contains(joined, ErrRemoteUnavailable.Error()) {
				t.Fatalf("failure is missing from the log = %v", lines)
			}
			if view.Dialog().Title != i18n.T(tc.key) {
				t.Fatalf("title = %q, want %q", view.Dialog().Title, i18n.T(tc.key))
			}
		})
	}
}
