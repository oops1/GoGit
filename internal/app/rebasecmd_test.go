package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/commit"
	"github.com/oops1/gogit/internal/ui/rebase"
)

func captureRebaseViews(t *testing.T) *[]*rebase.View {
	t.Helper()
	views := &[]*rebase.View{}
	prev := newRebaseView
	newRebaseView = func() (*rebase.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newRebaseView = prev })
	return views
}

func rebaseThrough(t *testing.T, a *App, run func()) []string {
	t.Helper()
	views := captureOperationViews(t)
	readOnDispatcher(t, a, func() bool { run(); return true })
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	waitForPostQueueDrain(t, a)
	waitForWorkingIdle(t, a)
	return readOnDispatcher(t, a, view.Lines)
}

func logHasPrefix(t *testing.T, lines []string, key string) bool {
	t.Helper()
	prefix, _, _ := strings.Cut(i18n.T(key), "%")
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func TestRebasingFromTheDialogReplaysTheBranch(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureRebaseViews(t)
	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdRebase) })
	view := (*views)[0]
	if got := readOnDispatcher(t, a, view.Onto); got != "feature" {
		t.Fatalf("onto = %q", got)
	}

	lines := rebaseThrough(t, a, func() { view.Dialog().DefaultAction() })

	if !logHasPrefix(t, lines, "Operation.Log.Rebased") {
		t.Fatalf("log = %v", lines)
	}
	if parents := headParents(t, target); len(parents) != 1 {
		t.Fatalf("parents = %v", parents)
	}
}

func TestAConflictedRebaseOffersContinueAndSkip(t *testing.T) {
	a, target := forkedApp(t, true)

	lines := rebaseThrough(t, a, func() { a.startRebase("feature") })

	if !logHasPrefix(t, lines, "Operation.Log.RebaseStopped") {
		t.Fatalf("log = %v", lines)
	}
	state := a.State()
	if !state.Rebasing || !state.Enabled(CmdSkip) || !state.Enabled(CmdContinue) || state.Enabled(CmdRebase) {
		t.Fatalf("state = %+v", state)
	}
	if text := waitForBanner(t, a, true); !strings.HasPrefix(text, strings.SplitN(i18n.T("Banner.Rebase.Conflicts"), "%", 2)[0]) {
		t.Fatalf("banner = %q", text)
	}
	if got := readOnDispatcher(t, a, func() string { return a.banner.commit.Text }); got != i18n.T("Banner.Rebase.Continue") {
		t.Fatalf("banner button = %q", got)
	}

	if err := ops.ResolveConflicts(t.Context(), gitrepoAt(t, target), []string{"f.txt"}, ops.TakeTheirs); err != nil {
		t.Fatal(err)
	}
	lines = rebaseThrough(t, a, func() { a.banner.commit.OnClick() })

	if !logHasPrefix(t, lines, "Operation.Log.Rebased") || a.State().Rebasing {
		t.Fatalf("log = %v, state = %+v", lines, a.State())
	}
	waitForBanner(t, a, false)
}

func gitrepoAt(t *testing.T, dir string) *gitrepo.Repository {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestSkippingARebaseStepDropsIt(t *testing.T) {
	a, target := forkedApp(t, true)
	rebaseThrough(t, a, func() { a.startRebase("feature") })

	lines := rebaseThrough(t, a, func() { a.Dispatch(CmdSkip) })

	if !logHasPrefix(t, lines, "Operation.Log.Rebased") || a.State().Rebasing {
		t.Fatalf("log = %v", lines)
	}
	if parents := headParents(t, target); len(parents) != 1 {
		t.Fatalf("parents = %v", parents)
	}
}

func TestARebaseThatCannotStartIsExplained(t *testing.T) {
	a, target := forkedApp(t, false)
	if err := writeFile(target, "f.txt", "dirty\n"); err != nil {
		t.Fatal(err)
	}

	lines := rebaseThrough(t, a, func() { a.startRebase("feature") })

	if !logHasPrefix(t, lines, "Operation.Log.RebaseBlocked") {
		t.Fatalf("log = %v", lines)
	}
}

func TestARebaseOfABranchAlreadyOnTopSaysSo(t *testing.T) {
	a, _ := forkedApp(t, false)

	lines := rebaseThrough(t, a, func() { a.startRebase("main") })

	if !logHasPrefix(t, lines, "Operation.Log.MergeUpToDate") {
		t.Fatalf("log = %v", lines)
	}
}

func TestContinueOutsideARebaseOpensTheCommitDialog(t *testing.T) {
	a, _ := forkedApp(t, false)
	opened := false
	a.showCommit = func(commit.Model, func(commit.Model, bool)) { opened = true }

	readOnDispatcher(t, a, func() bool { a.continueOperation(); return true })

	if !opened {
		t.Fatal("the commit dialog did not open")
	}
}

func TestRebaseCommandsNeedARepository(t *testing.T) {
	a := newTestApp(t)
	views := captureRebaseViews(t)

	a.openRebase("")
	a.startRebase("main")
	a.skipRebaseStep()

	if len(*views) != 0 {
		t.Fatal("a rebase ran without a repository")
	}
}

func TestRebaseDialogFailuresAreLogged(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := newRebaseView
	newRebaseView = func() (*rebase.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newRebaseView = prev })
	readOnDispatcher(t, a, func() bool { a.openRebase(""); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	readOnDispatcher(t, a, func() bool { a.openRebase(""); return true })
}

func TestCancellingTheRebaseDialogDoesNothing(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureRebaseViews(t)
	before := headParents(t, target)

	readOnDispatcher(t, a, func() bool { a.openRebase("feature"); return true })
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })

	if len(headParents(t, target)) != len(before) {
		t.Fatal("the branch moved")
	}
}

func TestAFailedRebaseKeepsTheRepository(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := runRebase
	runRebase = func(context.Context, *gitrepo.Repository, string, ops.RebaseOptions) (ops.RebaseResult, error) {
		return ops.RebaseResult{}, errors.New("no")
	}
	t.Cleanup(func() { runRebase = prev })

	rebaseThrough(t, a, func() { a.startRebase("feature") })

	if a.State().Rebasing {
		t.Fatal("a failed rebase left the state behind")
	}
}

func TestARebaseThatCannotOpenTheRepositoryFails(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prev })

	rebaseThrough(t, a, func() { a.startRebase("feature") })
}
