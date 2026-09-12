package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/commit"
	"github.com/oops1/gogit/internal/ui/rebase"
	"github.com/oops1/gogit/internal/ui/rebasetodo"
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

func captureTodoViews(t *testing.T) *[]*rebasetodo.View {
	t.Helper()
	views := &[]*rebasetodo.View{}
	prev := newRebaseTodoView
	newRebaseTodoView = func() (*rebasetodo.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newRebaseTodoView = prev })
	return views
}

func TestTheStepsDialogReplaysTheCommitsItWasGiven(t *testing.T) {
	a, target := forkedApp(t, false)
	rebaseViews := captureRebaseViews(t)
	todoViews := captureTodoViews(t)

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdRebaseSteps) })
	readOnDispatcher(t, a, func() bool { (*rebaseViews)[0].Dialog().DefaultAction(); return true })
	view := (*todoViews)[0]
	steps := readOnDispatcher(t, a, view.Steps)
	if len(steps) != 1 {
		t.Fatalf("steps = %+v", steps)
	}

	lines := rebaseThrough(t, a, func() { view.Dialog().DefaultAction() })

	if !logHasPrefix(t, lines, "Operation.Log.Rebased") {
		t.Fatalf("log = %v", lines)
	}
	if parents := headParents(t, target); len(parents) != 1 {
		t.Fatalf("parents = %v", parents)
	}
}

func TestDroppingEveryStepKeepsTheDialogOpen(t *testing.T) {
	a, _ := forkedApp(t, false)
	todoViews := captureTodoViews(t)
	readOnDispatcher(t, a, func() bool { a.openTodo("feature"); return true })
	view := (*todoViews)[0]

	readOnDispatcher(t, a, func() bool {
		dropped := view.Steps()
		for i := range dropped {
			dropped[i].Action = rebasetodo.ActionDrop
		}
		view.SetSteps("feature", dropped)
		view.Dialog().DefaultAction()
		return true
	})

	if a.State().Rebasing {
		t.Fatal("a todo with nothing to do started a rebase")
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestARewordStopsAndTheCommitDialogFinishesIt(t *testing.T) {
	a, target := forkedApp(t, false)
	todoViews := captureTodoViews(t)
	var asked commit.Model
	a.showCommit = func(initial commit.Model, cb func(commit.Model, bool)) {
		asked = initial
		cb(commit.Model{Message: "a reworded subject"}, false)
	}
	readOnDispatcher(t, a, func() bool { a.openTodo("feature"); return true })
	view := (*todoViews)[0]

	lines := rebaseThrough(t, a, func() {
		reworded := view.Steps()
		reworded[0].Action = rebasetodo.ActionReword
		view.SetSteps("feature", reworded)
		view.Dialog().DefaultAction()
	})

	if !logHasPrefix(t, lines, "Operation.Log.RebaseReword") {
		t.Fatalf("log = %v", lines)
	}
	if !a.State().Rewording {
		t.Fatal("the state does not say a message is wanted")
	}

	lines = rebaseThrough(t, a, func() { a.continueOperation() })

	if !logHasPrefix(t, lines, "Operation.Log.Rebased") || a.State().Rewording {
		t.Fatalf("log = %v, state = %+v", lines, a.State())
	}
	if asked.Message == "" || !asked.Merging {
		t.Fatalf("the commit dialog was asked with %+v", asked)
	}
	if subject := headSubject(t, target); subject != "a reworded subject" {
		t.Fatalf("subject = %q", subject)
	}
}

func TestTheStepsDialogNeedsARepositoryAndAPlan(t *testing.T) {
	a := newTestApp(t)
	todoViews := captureTodoViews(t)

	a.openRebaseSteps("")
	a.openTodo("feature")

	if len(*todoViews) != 0 {
		t.Fatal("the steps dialog opened without a repository")
	}

	b, _ := forkedApp(t, false)
	prevPlan := plannedRebase
	plannedRebase = func(context.Context, *gitrepo.Repository, string) ([]ops.RebaseStep, error) {
		return nil, errors.New("no plan")
	}
	t.Cleanup(func() { plannedRebase = prevPlan })
	readOnDispatcher(t, b, func() bool { b.openTodo("feature"); return true })

	prevView := newRebaseTodoView
	newRebaseTodoView = func() (*rebasetodo.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newRebaseTodoView = prevView })
	plannedRebase = prevPlan
	readOnDispatcher(t, b, func() bool { b.openTodo("feature"); return true })

	if len(*todoViews) != 0 {
		t.Fatal("a failing dialog was counted")
	}
}

func TestTheStepsDialogFailuresAreLogged(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := newRebaseView
	newRebaseView = func() (*rebase.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newRebaseView = prev })
	readOnDispatcher(t, a, func() bool { a.openRebaseSteps(""); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	readOnDispatcher(t, a, func() bool { a.openRebaseSteps(""); return true })
}

func TestTheStepsDialogGivesUpWhenTheRepositoryWillNotOpen(t *testing.T) {
	a, _ := forkedApp(t, false)
	todoViews := captureTodoViews(t)
	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prev })

	readOnDispatcher(t, a, func() bool { a.openTodo("feature"); return true })

	if len(*todoViews) != 0 {
		t.Fatal("the steps dialog opened without a repository")
	}
}

func TestCancellingTheFirstStepsDialogDoesNothing(t *testing.T) {
	a, target := forkedApp(t, false)
	rebaseViews := captureRebaseViews(t)
	todoViews := captureTodoViews(t)
	before := headParents(t, target)

	readOnDispatcher(t, a, func() bool { a.openRebaseSteps("feature"); return true })
	readOnDispatcher(t, a, func() bool { (*rebaseViews)[0].Dialog().CancelAction(); return true })

	if len(*todoViews) != 0 || len(headParents(t, target)) != len(before) {
		t.Fatal("cancelling the base dialog went on")
	}
}

func TestCancellingTheStepsDialogDoesNothing(t *testing.T) {
	a, target := forkedApp(t, false)
	rebaseViews := captureRebaseViews(t)
	todoViews := captureTodoViews(t)
	before := headParents(t, target)

	readOnDispatcher(t, a, func() bool { a.openRebaseSteps("feature"); return true })
	readOnDispatcher(t, a, func() bool { (*rebaseViews)[0].Dialog().DefaultAction(); return true })
	readOnDispatcher(t, a, func() bool { (*todoViews)[0].Dialog().CancelAction(); return true })

	if len(headParents(t, target)) != len(before) {
		t.Fatal("the branch moved")
	}
}

func headSubject(t *testing.T, target string) string {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	head, err := store.Resolve(refs.HEAD)
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.Commit(head.Target)
	if err != nil {
		t.Fatal(err)
	}
	subject, _, _ := strings.Cut(c.Message, "\n")
	return subject
}
