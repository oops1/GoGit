package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/worktree"
)

func newWorktreeTestApp(t *testing.T) (*App, string, string) {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, main, "main")
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: main}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	return a, main, filepath.Join(dir, "feature")
}

func captureWorktreeView(t *testing.T) **worktree.View {
	t.Helper()
	captured := new(*worktree.View)
	prev := newWorktreeView
	newWorktreeView = func(eng widget.ModalShower) (*worktree.View, error) {
		view, err := prev(eng)
		*captured = view
		return view, err
	}
	t.Cleanup(func() { newWorktreeView = prev })
	return captured
}

func stubAddWorktree(t *testing.T, replacement func(context.Context, *gitrepo.Repository, string, ops.AddWorktreeOptions) (ops.Worktree, error)) {
	t.Helper()
	prev := addWorktree
	addWorktree = replacement
	t.Cleanup(func() { addWorktree = prev })
}

func stubRemoveWorktree(t *testing.T, replacement func(context.Context, *gitrepo.Repository, string, bool) error) {
	t.Helper()
	prev := removeWorktree
	removeWorktree = replacement
	t.Cleanup(func() { removeWorktree = prev })
}

func stubPruneWorktrees(t *testing.T, replacement func(*gitrepo.Repository, ops.PruneWorktreesOptions) ([]string, error)) {
	t.Helper()
	prev := pruneWorktrees
	pruneWorktrees = replacement
	t.Cleanup(func() { pruneWorktrees = prev })
}

func answerConfirm(a *App, answers ...bool) *[]string {
	asked := new([]string)
	next := 0
	a.askConfirm = func(_, message string, cb func(bool)) {
		*asked = append(*asked, message)
		answer := false
		if next < len(answers) {
			answer = answers[next]
		}
		next++
		cb(answer)
	}
	return asked
}

func captureWorktreeMessages(a *App) (*[]string, *[]string) {
	info := new([]string)
	failures := new([]string)
	a.showInfo = func(_, message string) { *info = append(*info, message) }
	a.showError = func(_, message string) { *failures = append(*failures, message) }
	return info, failures
}

func waitOnDispatcher(t *testing.T, a *App, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !readOnDispatcher(t, a, condition) {
		if time.Now().After(deadline) {
			t.Fatal("the app did not reach the expected state in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheWorktreeDialogNeedsAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	opened := false
	prev := newWorktreeView
	newWorktreeView = func(widget.ModalShower) (*worktree.View, error) {
		opened = true
		return nil, nil
	}
	t.Cleanup(func() { newWorktreeView = prev })

	a.openAddWorktree()

	if opened {
		t.Fatal("without a repository there is nothing to add a worktree to")
	}
}

func TestAWorktreeDialogThatCannotOpenIsLogged(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	prev := newWorktreeView
	newWorktreeView = func(widget.ModalShower) (*worktree.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newWorktreeView = prev })

	a.openAddWorktree()
}

func TestTheWorktreeDialogOffersTheBranchesAndTheNeighbouringDirectory(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	captured := captureWorktreeView(t)

	a.openAddWorktree()

	view := *captured
	if view == nil {
		t.Fatal("the dialog was not opened")
	}
	if view.Known().Head != "main" {
		t.Fatalf("head = %q, want the current branch", view.Known().Head)
	}
	if view.ParentDirectory() != filepath.Dir(main) {
		t.Fatalf("parent = %q, want the directory next to the repository", view.ParentDirectory())
	}
}

func TestTheDialogListsTheBranchesThatOtherWorktreesHold(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{
			{Path: main, Main: true, Branch: "refs/heads/main"},
			{Path: linked, ID: "feature", Branch: "refs/heads/feature"},
		}, nil
	})
	captured := captureWorktreeView(t)

	a.openAddWorktree()

	known := (*captured).Known()
	if len(known.CheckedOut) != 2 || known.CheckedOut[1] != "feature" {
		t.Fatalf("checked out = %v, want both worktree branches", known.CheckedOut)
	}
}

func TestAWorktreeListThatCannotBeReadStillOpensTheDialog(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return nil, errors.New("no list")
	})
	captured := captureWorktreeView(t)

	a.openAddWorktree()

	if len((*captured).Known().CheckedOut) != 0 {
		t.Fatal("a failed list leaves the checked out branches unknown")
	}
}

func TestBranchesThatCannotBeReadStillOpenTheDialog(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) {
		return branches.Snapshot{}, errors.New("no branches")
	}
	t.Cleanup(func() { loadBranchSnapshot = prev })
	captured := captureWorktreeView(t)

	a.openAddWorktree()

	if (*captured).Known().Head != "" {
		t.Fatal("a failed snapshot leaves the head unknown")
	}
}

func TestTheModeDecidesWhatIsAskedOfTheCore(t *testing.T) {
	for _, c := range []struct {
		name string
		req  worktree.Request
		want ops.AddWorktreeOptions
	}{
		{
			name: "new branch",
			req:  worktree.Request{Mode: worktree.ModeNewBranch, Branch: " feature ", StartPoint: " main "},
			want: ops.AddWorktreeOptions{Branch: "feature", StartPoint: "main"},
		},
		{
			name: "existing branch",
			req:  worktree.Request{Mode: worktree.ModeExistingBranch, Branch: "feature", StartPoint: "ignored"},
			want: ops.AddWorktreeOptions{StartPoint: "feature"},
		},
		{
			name: "detached",
			req:  worktree.Request{Mode: worktree.ModeDetached, StartPoint: "v1.0.0", NoCheckout: true},
			want: ops.AddWorktreeOptions{StartPoint: "v1.0.0", Detach: true, NoCheckout: true},
		},
	} {
		got := worktreeAddOptions(c.req, nil)
		if got.Branch != c.want.Branch || got.StartPoint != c.want.StartPoint ||
			got.Detach != c.want.Detach || got.NoCheckout != c.want.NoCheckout {
			t.Fatalf("%s: options = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestAddingAWorktreePutsItInTheTreeAndOpensIt(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	initTestWorktree(t, main, linked, "feature")
	stubAddWorktree(t, func(context.Context, *gitrepo.Repository, string, ops.AddWorktreeOptions) (ops.Worktree, error) {
		return ops.Worktree{ID: "feature", Path: linked, Branch: "refs/heads/feature"}, nil
	})
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{
			{Path: main, Main: true},
			{Path: linked, ID: "feature", Branch: "refs/heads/feature"},
		}, nil
	})

	a.startAddWorktree(worktree.Request{Path: linked, Branch: "feature"})

	waitOnDispatcher(t, a, func() bool {
		node, ok := a.registry.FindByPath(linked)
		return ok && node.Kind == repo.KindWorktree && a.State().ActiveRepository == node.ID
	})
}

func TestAddingAWorktreeThatFailsChangesNothing(t *testing.T) {
	a, _, linked := newWorktreeTestApp(t)
	tried := make(chan struct{}, 1)
	stubAddWorktree(t, func(context.Context, *gitrepo.Repository, string, ops.AddWorktreeOptions) (ops.Worktree, error) {
		tried <- struct{}{}
		return ops.Worktree{}, errors.New("no worktree for you")
	})

	a.startAddWorktree(worktree.Request{Path: linked, Branch: "feature"})

	<-tried
	drainPostQueue(t, a)
	if _, ok := a.registry.FindByPath(linked); ok {
		t.Fatal("a failed add must leave the tree alone")
	}
}

func TestAddingAWorktreeNeedsAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	called := false
	stubAddWorktree(t, func(context.Context, *gitrepo.Repository, string, ops.AddWorktreeOptions) (ops.Worktree, error) {
		called = true
		return ops.Worktree{}, nil
	})

	a.startAddWorktree(worktree.Request{Path: t.TempDir()})

	if called {
		t.Fatal("without a repository there is nothing to add to")
	}
}

func TestTheDialogStartsTheAddAndTheCancelClosesIt(t *testing.T) {
	a, _, linked := newWorktreeTestApp(t)
	started := make(chan string, 1)
	stubAddWorktree(t, func(_ context.Context, _ *gitrepo.Repository, path string, _ ops.AddWorktreeOptions) (ops.Worktree, error) {
		started <- path
		return ops.Worktree{}, errors.New("stop here")
	})
	captured := captureWorktreeView(t)
	a.openAddWorktree()

	(*captured).OnOK(worktree.Request{Path: linked, Branch: "feature"})

	if got := <-started; got != linked {
		t.Fatalf("add started for %q, want %q", got, linked)
	}
	drainPostQueue(t, a)

	a.openAddWorktree()
	(*captured).OnCancel()
}

func TestRemovingIsOnlyOfferedForAWorktree(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	asked := answerConfirm(a, true)

	a.removeActiveWorktree()

	if len(*asked) != 0 {
		t.Fatal("a repository is not removed by the worktree command")
	}
}

func worktreeInTree(t *testing.T, a *App, main, linked string) *repo.Node {
	t.Helper()
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	initTestWorktree(t, main, linked, "feature")
	node, err := a.registry.AddWorktree("r1", "feature", linked)
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)
	return node
}

func TestRemovingAWorktreeAsksFirstAndDropsItFromTheTree(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	node := worktreeInTree(t, a, main, linked)
	asked := answerConfirm(a, true)
	var forced []bool
	stubRemoveWorktree(t, func(_ context.Context, _ *gitrepo.Repository, path string, force bool) error {
		forced = append(forced, force)
		return os.RemoveAll(path)
	})
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: main, Main: true}}, nil
	})

	a.removeActiveWorktree()

	if len(*asked) != 1 || len(forced) != 1 || forced[0] {
		t.Fatalf("asked = %v, forced = %v, want one plain removal", *asked, forced)
	}
	if _, ok := a.registry.Find(node.ID); ok {
		t.Fatal("the removed worktree must leave the tree")
	}
}

func TestADeclinedQuestionKeepsTheWorktree(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	node := worktreeInTree(t, a, main, linked)
	answerConfirm(a, false)
	stubRemoveWorktree(t, func(context.Context, *gitrepo.Repository, string, bool) error {
		t.Error("a declined question must not remove anything")
		return nil
	})

	a.removeActiveWorktree()

	if _, ok := a.registry.Find(node.ID); !ok {
		t.Fatal("the worktree must stay in the tree")
	}
}

func TestAWorktreeWithChangesIsRemovedOnlyAfterASecondQuestion(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	worktreeInTree(t, a, main, linked)
	asked := answerConfirm(a, true, true)
	var forced []bool
	stubRemoveWorktree(t, func(_ context.Context, _ *gitrepo.Repository, _ string, force bool) error {
		forced = append(forced, force)
		if !force {
			return ops.ErrWorktreeDirty
		}
		return nil
	})
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: main, Main: true}}, nil
	})

	a.removeActiveWorktree()

	if len(*asked) != 2 || len(forced) != 2 || !forced[1] {
		t.Fatalf("asked %d times, forced = %v, want a forced second attempt", len(*asked), forced)
	}
}

func TestADeclinedForceLeavesTheWorktreeAlone(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	node := worktreeInTree(t, a, main, linked)
	answerConfirm(a, true, false)
	stubRemoveWorktree(t, func(context.Context, *gitrepo.Repository, string, bool) error {
		return ops.ErrWorktreeDirty
	})

	a.removeActiveWorktree()

	if _, ok := a.registry.Find(node.ID); !ok {
		t.Fatal("the worktree must stay when the force is declined")
	}
}

func TestAFailedRemovalIsReported(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	worktreeInTree(t, a, main, linked)
	answerConfirm(a, true)
	_, failures := captureWorktreeMessages(a)
	stubRemoveWorktree(t, func(context.Context, *gitrepo.Repository, string, bool) error {
		return errors.New("it is stuck")
	})

	a.removeActiveWorktree()

	if len(*failures) != 1 {
		t.Fatalf("failures = %v, want the removal failure", *failures)
	}
}

func TestRemovingNeedsARepositoryToRemoveFrom(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: filepath.Join(dir, "gone")},
		{ID: "w1", Name: "feature", Path: filepath.Join(dir, "feature"), Worktree: true, Parent: "r1"},
	}
	a := newTestAppWithConfig(t, cfg)
	if err := a.registry.SetActive("w1"); err != nil {
		t.Fatal(err)
	}
	answerConfirm(a, true)
	stubRemoveWorktree(t, func(context.Context, *gitrepo.Repository, string, bool) error {
		t.Error("a repository that cannot be opened has nothing to remove")
		return nil
	})

	a.removeActiveWorktree()
}

func TestAWorktreeWithoutAParentIsNotRemoved(t *testing.T) {
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "w1", Name: "feature", Path: filepath.Join(t.TempDir(), "feature"), Worktree: true},
	}
	a := newTestAppWithConfig(t, cfg)
	if err := a.registry.SetActive("w1"); err != nil {
		t.Fatal(err)
	}
	asked := answerConfirm(a, true)

	a.removeActiveWorktree()

	if len(*asked) != 0 {
		t.Fatal("a worktree without a parent repository cannot be removed")
	}
}

func TestPruningNeedsAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	stubPruneWorktrees(t, func(*gitrepo.Repository, ops.PruneWorktreesOptions) ([]string, error) {
		t.Error("without a repository there is nothing to prune")
		return nil, nil
	})

	a.pruneObsoleteWorktrees()
}

func TestPruningWithNothingStaleSaysSo(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	info, _ := captureWorktreeMessages(a)
	stubPruneWorktrees(t, func(*gitrepo.Repository, ops.PruneWorktreesOptions) ([]string, error) {
		return nil, nil
	})

	a.pruneObsoleteWorktrees()

	if len(*info) != 1 {
		t.Fatalf("info = %v, want the nothing-to-prune notice", *info)
	}
}

func TestPruningAsksBeforeItDropsTheRecords(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	info, _ := captureWorktreeMessages(a)
	asked := answerConfirm(a, true)
	var dry []bool
	stubPruneWorktrees(t, func(_ *gitrepo.Repository, opts ops.PruneWorktreesOptions) ([]string, error) {
		dry = append(dry, opts.DryRun)
		return []string{"feature", "old"}, nil
	})

	a.pruneObsoleteWorktrees()

	if len(*asked) != 1 || len(dry) != 2 || !dry[0] || dry[1] {
		t.Fatalf("asked = %v, dry runs = %v, want a dry run and then the real one", *asked, dry)
	}
	if len(*info) != 1 {
		t.Fatalf("info = %v, want the count of removed records", *info)
	}
}

func TestADeclinedPruneRemovesNothing(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	answerConfirm(a, false)
	var dry []bool
	stubPruneWorktrees(t, func(_ *gitrepo.Repository, opts ops.PruneWorktreesOptions) ([]string, error) {
		dry = append(dry, opts.DryRun)
		return []string{"feature"}, nil
	})

	a.pruneObsoleteWorktrees()

	if len(dry) != 1 {
		t.Fatalf("dry runs = %v, want the dry run alone", dry)
	}
}

func TestAFailedDryRunIsReported(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	_, failures := captureWorktreeMessages(a)
	stubPruneWorktrees(t, func(*gitrepo.Repository, ops.PruneWorktreesOptions) ([]string, error) {
		return nil, errors.New("cannot read")
	})

	a.pruneObsoleteWorktrees()

	if len(*failures) != 1 {
		t.Fatalf("failures = %v, want the prune failure", *failures)
	}
}

func TestAFailedPruneIsReported(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	_, failures := captureWorktreeMessages(a)
	answerConfirm(a, true)
	stubPruneWorktrees(t, func(_ *gitrepo.Repository, opts ops.PruneWorktreesOptions) ([]string, error) {
		if opts.DryRun {
			return []string{"feature"}, nil
		}
		return nil, errors.New("cannot delete")
	})

	a.pruneObsoleteWorktrees()

	if len(*failures) != 1 {
		t.Fatalf("failures = %v, want the prune failure", *failures)
	}
}

func TestTheWorktreeCommandsAreWired(t *testing.T) {
	a := newTestApp(t)

	for _, id := range []CommandID{CmdAddWorktree, CmdRemoveWorktree, CmdPruneWorktrees} {
		if a.handlers[id] == nil {
			t.Fatalf("command %q has no handler", id)
		}
	}
}

func TestTheDialogListsTheLocalBranches(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) {
		return branches.Snapshot{
			Current: "main",
			Local: []branches.Branch{
				{Name: refs.BranchName("main")},
				{Name: refs.BranchName("feature")},
			},
		}, nil
	}
	t.Cleanup(func() { loadBranchSnapshot = prev })
	captured := captureWorktreeView(t)

	a.openAddWorktree()

	known := (*captured).Known()
	if len(known.Branches) != 2 || known.Branches[1] != "feature" {
		t.Fatalf("branches = %v, want the local ones", known.Branches)
	}
}

func TestWithoutAnActiveNodeThereIsNoParentForAWorktree(t *testing.T) {
	a := newTestApp(t)

	if id := a.worktreeParentID(); id != "" {
		t.Fatalf("parent = %q, want none", id)
	}
}

func TestRegisteringAnAddedWorktreeNeedsAnOpenRepository(t *testing.T) {
	a := newTestApp(t)

	a.registerAddedWorktree("r1", filepath.Join(t.TempDir(), "feature"))
}

func TestAWorktreeThatDidNotReachTheTreeIsNotOpened(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: main, Main: true}}, nil
	})

	a.registerAddedWorktree("r1", linked)

	if a.State().ActiveRepository != "r1" {
		t.Fatal("the repository must stay open when the worktree did not reach the tree")
	}
}

func TestAConfigThatCannotBeSavedAfterAWorktreeChangeIsLogged(t *testing.T) {
	a, main, linked := newWorktreeTestApp(t)
	initTestWorktree(t, main, linked, "feature")
	if err := os.RemoveAll(a.paths.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.paths.ConfigFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{
			{Path: main, Main: true},
			{Path: linked, ID: "feature", Branch: "refs/heads/feature"},
		}, nil
	})

	a.saveWorktreeTree("r1", a.opened())

	if _, ok := a.registry.FindByPath(linked); !ok {
		t.Fatal("the worktree must reach the tree even when the config cannot be saved")
	}
}
