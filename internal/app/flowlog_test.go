package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/flow"
)

func openFinishForLog(t *testing.T, a *App) *flow.FinishView {
	t.Helper()
	finishes := captureKindViews(t, &newFlowFinishView)
	runOnDispatcher(t, a, func() {
		a.setFlowState(flowOn(featureBranch, FlowBranch{}))
		a.Dispatch(CmdFlowFinishFeature)
	})
	return waitForFlowView(t, a, finishes, 1)
}

func TestSelectFromLogPutsTheMessageOfTheChosenCommitInTheFinishDialog(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	logs := captureFlowViews(t, &newFlowLogView)
	finish := openFinishForLog(t, a)
	before := readOnDispatcher(t, a, func() string { return finish.Model().Message })

	runOnDispatcher(t, a, finish.OnSelectFromLog)
	picker := waitForFlowView(t, a, logs, 1)
	commits := readOnDispatcher(t, a, picker.Commits)
	if len(commits) == 0 {
		t.Fatal("the picker offers no commits")
	}
	runOnDispatcher(t, a, func() { picker.OnOK(commits[0]) })

	if got := readOnDispatcher(t, a, func() string { return finish.Model().Message }); got != commits[0].Message {
		t.Fatalf("message = %q, want %q", got, commits[0].Message)
	}

	runOnDispatcher(t, a, finish.OnSelectFromLog)
	second := waitForFlowView(t, a, logs, 2)
	runOnDispatcher(t, a, second.OnCancel)

	if got := readOnDispatcher(t, a, func() string { return finish.Model().Message }); got != commits[0].Message {
		t.Fatalf("a cancelled pick changed the message to %q", got)
	}
	if before == commits[0].Message {
		t.Fatalf("the default message already was %q, the test proves nothing", before)
	}
}

func TestTheLogPickerOffersTheCommitsOfTheCurrentBranch(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	commits := readOnDispatcher(t, a, func() []flow.LogCommit {
		loaded, err := readFlowLog(t.Context(), a.opened(), 1)
		if err != nil {
			t.Error(err)
		}
		return loaded
	})

	if len(commits) != 1 {
		t.Fatalf("commits = %d, want the newest one only", len(commits))
	}
	if commits[0].Message == "" || commits[0].Author == "" || commits[0].Commit.IsZero() {
		t.Fatalf("commit = %+v", commits[0])
	}
}

func TestTheLogPickerIsEmptyOnAnUnbornBranch(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fresh")
	initTestRepo(t, target)
	a := activatedWorkingApp(t, target)

	commits := readOnDispatcher(t, a, func() []flow.LogCommit {
		loaded, err := readFlowLog(t.Context(), a.opened(), 10)
		if err != nil {
			t.Error(err)
		}
		return loaded
	})

	if len(commits) != 0 {
		t.Fatalf("commits = %+v", commits)
	}
}

func TestTheLogPickerReportsABrokenHead(t *testing.T) {
	for name, head := range map[string]string{
		"malformed": "not a reference at all\n",
		"missing":   "0123456789012345678901234567890123456789\n",
	} {
		a, target := flowReadyApp(t, true)
		if err := os.WriteFile(filepath.Join(target, ".git", "HEAD"), []byte(head), 0o666); err != nil {
			t.Fatal(err)
		}
		commits, err := readOnDispatcher(t, a, func() flowLogResult {
			loaded, err := readFlowLog(t.Context(), a.opened(), 10)
			return flowLogResult{loaded, err}
		}).split()

		if err == nil {
			t.Fatalf("%s HEAD: commits = %+v, want an error", name, commits)
		}
	}
}

type flowLogResult struct {
	commits []flow.LogCommit
	err     error
}

func (r flowLogResult) split() ([]flow.LogCommit, error) { return r.commits, r.err }

func TestAFailedLogReadIsReported(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	logs := captureFlowViews(t, &newFlowLogView)
	finish := openFinishForLog(t, a)
	prev := readFlowLog
	readFlowLog = func(context.Context, *openedRepository, int) ([]flow.LogCommit, error) {
		return nil, errors.New("no objects")
	}
	t.Cleanup(func() { readFlowLog = prev })

	runOnDispatcher(t, a, finish.OnSelectFromLog)

	waitForStatusText(t, a, i18n.Tf("Status.FlowLogFailed", errors.New("no objects")))
	if len(*logs) != 0 {
		t.Fatalf("pickers opened = %d", len(*logs))
	}
}

func TestTheLogPickerThatCannotOpenIsLeftAlone(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	failFlowView(t, &newFlowLogView)
	shown := readOnDispatcher(t, a, func() int { return len(a.shownDialogs) })

	runOnDispatcher(t, a, func() { a.showFlowLog(nil, nil) })

	if got := readOnDispatcher(t, a, func() int { return len(a.shownDialogs) }); got != shown {
		t.Fatalf("dialogs shown = %d, want %d", got, shown)
	}
}

func TestTheLogPickerNeedsAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	logs := captureFlowViews(t, &newFlowLogView)

	runOnDispatcher(t, a, func() { a.openFlowLog(nil) })

	if len(*logs) != 0 {
		t.Fatalf("pickers opened = %d", len(*logs))
	}
}
