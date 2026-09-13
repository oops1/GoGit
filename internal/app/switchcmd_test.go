package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/switchbranch"
)

func captureSwitchViews(t *testing.T) *[]*switchbranch.View {
	t.Helper()
	views := &[]*switchbranch.View{}
	prev := newSwitchView
	newSwitchView = func() (*switchbranch.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newSwitchView = prev })
	return views
}

func branchNow(t *testing.T, a *App) string {
	t.Helper()
	return readOnDispatcher(t, a, a.currentBranchName)
}

func TestSwitchingToAnotherBranchMovesTheWorkingCopy(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureSwitchViews(t)

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdSwitch) })
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool { view.SetKnown(switchKnown(branchSnapshot(t, a)), "feature"); return true })

	lines := rebaseThrough(t, a, func() { view.Dialog().DefaultAction() })

	if !logHasPrefix(t, lines, "Operation.Log.Switched") {
		t.Fatalf("log = %v", lines)
	}
	if got := branchNow(t, a); got != "feature" {
		t.Fatalf("branch = %q", got)
	}
}

func branchSnapshot(t *testing.T, a *App) branches.Snapshot {
	t.Helper()
	o := a.opened()
	if o == nil {
		t.Fatal("no repository is open")
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestADoubleClickChecksOutALocalBranchAtOnce(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureSwitchViews(t)

	readOnDispatcher(t, a, func() bool { a.checkOutRef(refs.BranchName("main")); return true })
	lines := rebaseThrough(t, a, func() { a.checkOutRef(refs.BranchName("feature")) })

	if !logHasPrefix(t, lines, "Operation.Log.Switched") || branchNow(t, a) != "feature" || len(*views) != 0 {
		t.Fatalf("log = %v, branch = %q, dialogs = %d", lines, branchNow(t, a), len(*views))
	}
}

func TestADoubleClickOnARemoteBranchOrATagAsksHowToCheckItOut(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureSwitchViews(t)

	readOnDispatcher(t, a, func() bool { a.checkOutRef("refs/remotes/origin/main"); return true })

	if len(*views) != 1 {
		t.Fatal("the switch dialog did not open")
	}
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })
}

func TestTheBranchMenuIsEmptyForAnythingButBranchesAndTags(t *testing.T) {
	a, _ := forkedApp(t, false)

	if items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu("refs/stash") }); items != nil {
		t.Fatalf("items = %v", menuTexts(items))
	}
	if item, _ := findMenuItem(readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(refs.BranchName("main")) }), i18n.T("Menu.Ref.CheckOut")); !item.Disabled {
		t.Fatal("the current branch cannot be checked out again")
	}
}

func TestStartingALocalBranchFromARemoteOne(t *testing.T) {
	a, target := forkedApp(t, false)
	var asked ops.StartBranchOptions
	prev := runStartBranch
	runStartBranch = func(ctx context.Context, r *gitrepo.Repository, name, start string, opts ops.StartBranchOptions) (ops.StartBranchResult, error) {
		asked = opts
		return ops.StartBranchResult{Branch: refs.BranchName(name)}, nil
	}
	t.Cleanup(func() { runStartBranch = prev })

	lines := rebaseThrough(t, a, func() {
		a.startSwitch(switchbranch.Choice{Source: "origin/fresh", Name: "fresh", Track: true})
	})

	if !logHasPrefix(t, lines, "Operation.Log.Switched") || !asked.Track {
		t.Fatalf("log = %v, options = %+v", lines, asked)
	}
	if got := branchNow(t, a); got != "main" {
		t.Fatalf("branch = %q in %s", got, target)
	}
}

func TestASwitchOverLocalChangesIsExplained(t *testing.T) {
	a, target := forkedApp(t, false)
	if err := writeFile(target, "f.txt", "dirty\n"); err != nil {
		t.Fatal(err)
	}

	lines := rebaseThrough(t, a, func() { a.startSwitch(switchbranch.Choice{Source: "feature"}) })

	if !logHasPrefix(t, lines, "Operation.Log.SwitchBlocked") {
		t.Fatalf("log = %v", lines)
	}
}

func TestAFailedSwitchKeepsTheBranch(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := runSwitchBranch
	runSwitchBranch = func(context.Context, *gitrepo.Repository, string, ops.SwitchOptions) error {
		return errors.New("no")
	}
	t.Cleanup(func() { runSwitchBranch = prev })

	rebaseThrough(t, a, func() { a.startSwitch(switchbranch.Choice{Source: "feature"}) })

	if got := branchNow(t, a); got != "main" {
		t.Fatalf("branch = %q", got)
	}
}

func TestTheSwitchCommandsNeedARepository(t *testing.T) {
	a := newTestApp(t)
	views := captureSwitchViews(t)

	a.openSwitch("")
	a.startSwitch(switchbranch.Choice{Source: "feature"})

	if len(*views) != 0 {
		t.Fatal("the switch dialog opened without a repository")
	}
}

func TestTheSwitchDialogFailuresAreLogged(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := newSwitchView
	newSwitchView = func() (*switchbranch.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newSwitchView = prev })
	readOnDispatcher(t, a, func() bool { a.openSwitch(""); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	readOnDispatcher(t, a, func() bool { a.openSwitch(""); return true })
}

func TestASwitchThatCannotOpenTheRepositoryFails(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prev })

	rebaseThrough(t, a, func() { a.startSwitch(switchbranch.Choice{Source: "feature"}) })
}

func TestTheSwitchDialogSeesRemoteBranchesToo(t *testing.T) {
	snap := branches.Snapshot{
		Current: "main",
		Local:   []branches.Branch{{Name: refs.BranchName("main")}},
		Remotes: []branches.Remote{{Name: "origin", Branches: []branches.Branch{{Name: refs.RemoteBranchName("origin", "fresh")}}}},
	}

	known := switchKnown(snap)

	if len(known.Local) != 1 || len(known.Remote) != 1 || known.Remote[0] != "origin/fresh" {
		t.Fatalf("known = %+v", known)
	}
}
