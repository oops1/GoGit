package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/i18n"
)

func operationLogOfPushTargets(t *testing.T, a *App, targets []remote.TargetResult) []string {
	t.Helper()
	views := captureOperationViews(t)
	a.RunOperation("Push", func(_ context.Context, reporter OperationReporter) error {
		logPushTargets(reporter, targets)
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	return readOnDispatcher(t, a, view.Lines)
}

func TestTheOperationLogTellsWhatEachPushAddressGot(t *testing.T) {
	a := newTestApp(t)

	lines := operationLogOfPushTargets(t, a, []remote.TargetResult{
		{
			URL:  "https://valeriy:secret@hub.example/repo.git",
			Sent: []remote.PushUpdate{{Source: refs.BranchName("main"), Target: refs.BranchName("main")}},
		},
		{URL: "http://mirror.example/repo.git"},
		{URL: "http://down.example/repo.git", Err: errors.New("connection refused")},
	})

	want := []string{
		i18n.Tf("Operation.Log.PushTargetDone", "https://valeriy:***@hub.example/repo.git", "main"),
		i18n.Tf("Operation.Log.PushTargetUpToDate", "http://mirror.example/repo.git"),
		i18n.Tf("Operation.Log.PushTargetFailed", "http://down.example/repo.git", "connection refused"),
	}
	for _, line := range want {
		if !slices.Contains(lines, line) {
			t.Fatalf("lines = %q, want %q", lines, line)
		}
	}
}

func TestThePushLogHidesAPasswordThatLeakedIntoTheError(t *testing.T) {
	a := newTestApp(t)

	lines := operationLogOfPushTargets(t, a, []remote.TargetResult{
		{URL: "http://one.example/repo.git", Err: errors.New("https://valeriy:secret@hub.example/repo.git: connection refused")},
		{URL: "http://two.example/repo.git"},
	})

	for _, line := range lines {
		if strings.Contains(line, "secret") {
			t.Fatalf("the password leaked into the log: %q", line)
		}
	}
}
