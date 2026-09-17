package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/stash"
)

func captureStashViews(t *testing.T) *[]*stash.View {
	t.Helper()
	views := &[]*stash.View{}
	prev := newStashView
	newStashView = func(mode stash.Mode) (*stash.View, error) {
		view, err := prev(mode)
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newStashView = prev })
	return views
}

func stashMessages(t *testing.T, target string) []string {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	entries, err := ops.StashList(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]string, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}
	return messages
}

func stashedApp(t *testing.T) (*App, string) {
	t.Helper()
	a, target := blockedSwitchApp(t, "dirty\n")
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.StashPush(t.Context(), r, ops.StashOptions{Message: "keep"}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	runOnDispatcher(t, a, a.RefreshRepository)
	return a, target
}

func hasStashes(t *testing.T, a *App) bool {
	t.Helper()
	return readOnDispatcher(t, a, func() bool { return a.State().HasStashes })
}

func TestSavingAStashPutsTheChangesAside(t *testing.T) {
	a, target := blockedSwitchApp(t, "dirty\n")
	runOnDispatcher(t, a, func() {
		a.askInput = func(_, _ string, cb func(string, bool)) { cb("  keep  ", true) }
	})

	lines := rebaseThrough(t, a, a.openSaveStash)

	if !logHasPrefix(t, lines, "Operation.Log.StashSaved") {
		t.Fatalf("log = %v", lines)
	}
	if readWorkingFile(t, target, "f.txt") == "dirty\n" {
		t.Fatal("the change stayed in the working tree")
	}
	if got := stashMessages(t, target); len(got) != 1 || got[0] != "On main: keep" {
		t.Fatalf("stashes = %v", got)
	}
	shown := readOnDispatcher(t, a, func() bool { _, ok := a.branchesView.Item(branches.StashRef(0)); return ok })
	if !shown || !hasStashes(t, a) {
		t.Fatal("the branches pane does not show the stash")
	}
}

func TestSavingWithoutChangesSaysThereIsNothingToStash(t *testing.T) {
	a, _ := forkedApp(t, false)

	lines := rebaseThrough(t, a, func() { a.saveStash("") })

	if !logHasPrefix(t, lines, "Operation.Log.NothingToStash") {
		t.Fatalf("log = %v", lines)
	}
}

func TestCancellingTheStashMessageSavesNothing(t *testing.T) {
	a, target := blockedSwitchApp(t, "dirty\n")
	views := captureOperationViews(t)

	runOnDispatcher(t, a, func() {
		a.askInput = func(_, _ string, cb func(string, bool)) { cb("x", false) }
		a.openSaveStash()
	})

	if len(*views) != 0 || stashCount(t, target) != 0 {
		t.Fatal("a cancelled message still saved a stash")
	}
}

func TestStashCommandsNeedARepository(t *testing.T) {
	a := newTestApp(t)
	operations := captureOperationViews(t)
	views := captureStashViews(t)

	a.openSaveStash()
	a.saveStash("")
	a.openStashDialog(stash.ModeApply, 0)
	a.applyStash(0, false)

	if len(*operations) != 0 || len(*views) != 0 {
		t.Fatal("a stash command ran without a repository")
	}
}

func TestSaveStashFailuresAreReported(t *testing.T) {
	a, _ := blockedSwitchApp(t, "dirty\n")
	prev := runStashPush
	runStashPush = func(context.Context, *gitrepo.Repository, ops.StashOptions) (hash.ObjectID, error) {
		return hash.Zero, errors.New("no stash")
	}
	t.Cleanup(func() { runStashPush = prev })

	lines := rebaseThrough(t, a, func() { a.saveStash("") })
	if logHasPrefix(t, lines, "Operation.Log.StashSaved") {
		t.Fatalf("log = %v", lines)
	}

	prevOpen := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prevOpen })
	rebaseThrough(t, a, func() { a.saveStash("") })
}

func TestApplyingAStashFromTheDialogCanDropIt(t *testing.T) {
	a, target := stashedApp(t)
	views := captureStashViews(t)

	runOnDispatcher(t, a, func() { a.Dispatch(CmdStashApply) })
	if len(*views) != 1 {
		t.Fatalf("apply dialogs = %d", len(*views))
	}
	view := (*views)[0]
	lines := rebaseThrough(t, a, func() { view.OnOK(stash.Request{Index: 0, Drop: true}) })

	if !logHasPrefix(t, lines, "Operation.Log.StashApplied") || !logHasPrefix(t, lines, "Operation.Log.StashDropped") {
		t.Fatalf("log = %v", lines)
	}
	if readWorkingFile(t, target, "f.txt") != "dirty\n" || stashCount(t, target) != 0 || hasStashes(t, a) {
		t.Fatal("the stash was not applied and dropped")
	}
}

func TestDroppingAStashFromTheDialog(t *testing.T) {
	a, target := stashedApp(t)
	views := captureStashViews(t)

	runOnDispatcher(t, a, func() { a.Dispatch(CmdStashDrop) })
	view := (*views)[0]
	runOnDispatcher(t, a, func() { view.OnOK(stash.Request{Index: 0, Drop: true}) })
	waitForStatusText(t, a, i18n.Tf("Status.StashDropped", "stash@{0}"))

	if stashCount(t, target) != 0 || readWorkingFile(t, target, "f.txt") == "dirty\n" {
		t.Fatal("drop applied the stash or kept it")
	}
}

func TestCancellingTheStashDialogKeepsTheStash(t *testing.T) {
	a, target := stashedApp(t)
	views := captureStashViews(t)

	runOnDispatcher(t, a, func() { a.Dispatch(CmdStashSave) })
	runOnDispatcher(t, a, func() { a.activateRef(branches.StashRef(0)) })
	runOnDispatcher(t, a, func() { a.activateRef(refs.BranchName("main")) })
	if len(*views) != 1 {
		t.Fatalf("apply dialogs = %d", len(*views))
	}
	runOnDispatcher(t, a, func() { (*views)[0].Dialog().CancelAction() })

	if stashCount(t, target) != 1 {
		t.Fatal("cancelling removed the stash")
	}
}

func TestTheStashMenuAppliesAndDropsAnEntry(t *testing.T) {
	a, target := stashedApp(t)
	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.refMenu(branches.StashRef(0)) })
	apply, _ := findMenuItem(items, i18n.T("Menu.Stash.Apply"))
	applyDrop, _ := findMenuItem(items, i18n.T("Menu.Stash.ApplyDrop"))
	drop, _ := findMenuItem(items, i18n.T("Menu.Stash.Drop"))

	lines := rebaseThrough(t, a, apply.OnClick)
	if !logHasPrefix(t, lines, "Operation.Log.StashApplied") || stashCount(t, target) != 1 {
		t.Fatalf("log = %v", lines)
	}

	answer := false
	runOnDispatcher(t, a, func() { a.askConfirm = func(_, _ string, cb func(bool)) { cb(answer) } })
	runOnDispatcher(t, a, drop.OnClick)
	if stashCount(t, target) != 1 {
		t.Fatal("a refused confirmation dropped the stash")
	}
	answer = true
	runOnDispatcher(t, a, drop.OnClick)
	waitForStatusText(t, a, i18n.Tf("Status.StashDropped", "stash@{0}"))

	lines = rebaseThrough(t, a, applyDrop.OnClick)
	if logHasPrefix(t, lines, "Operation.Log.StashApplied") {
		t.Fatalf("a missing stash was applied: %v", lines)
	}
	if branch := readOnDispatcher(t, a, func() []widget.MenuItem { return a.refMenu(refs.BranchName("feature")) }); len(branch) == 0 {
		t.Fatal("branches lost their menu")
	}
}

func TestApplyingAStashExplainsBlockedAndConflictingChanges(t *testing.T) {
	a, _ := stashedApp(t)
	prev := runStashApply
	t.Cleanup(func() { runStashApply = prev })
	cases := []struct {
		key    string
		result ops.StashApplyResult
		err    error
	}{
		{"Operation.Log.StashBlocked", ops.StashApplyResult{}, &ops.OverwriteError{Paths: []string{"f.txt"}}},
		{"Operation.Log.StashConflicts", ops.StashApplyResult{Conflicts: []string{"f.txt"}}, nil},
	}
	for _, c := range cases {
		runStashApply = func(context.Context, *gitrepo.Repository, int, ops.StashApplyOptions) (ops.StashApplyResult, error) {
			return c.result, c.err
		}
		lines := rebaseThrough(t, a, func() { a.applyStash(0, false) })
		if !logHasPrefix(t, lines, c.key) {
			t.Fatalf("%s missing from %v", c.key, lines)
		}
	}

	prevOpen := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prevOpen })
	rebaseThrough(t, a, func() { a.applyStash(0, false) })
}

func TestStashDialogFailuresAreLogged(t *testing.T) {
	a, _ := stashedApp(t)
	prev := newStashView
	newStashView = func(stash.Mode) (*stash.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newStashView = prev })
	runOnDispatcher(t, a, func() { a.openStashDialog(stash.ModeApply, 0) })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	runOnDispatcher(t, a, func() { a.openStashDialog(stash.ModeDrop, 0) })
}

func TestADropThatFailsIsShown(t *testing.T) {
	a, target := stashedApp(t)
	prev := runStashDrop
	runStashDrop = func(context.Context, *gitrepo.Repository, int) error { return errors.New("locked") }
	t.Cleanup(func() { runStashDrop = prev })

	runOnDispatcher(t, a, func() { a.dropStash(0) })
	waitForStatusText(t, a, i18n.Tf("Status.StashDropFailed", errors.New("locked")))

	if stashCount(t, target) != 1 {
		t.Fatal("a failed drop removed the stash")
	}
}

func TestStashCommandsFollowTheRepositoryState(t *testing.T) {
	s := State{ActiveRepository: "r"}
	if s.Enabled(CmdStashSave) || s.Enabled(CmdStashApply) || s.Enabled(CmdStashDrop) {
		t.Fatal("stash commands are on without changes or stashes")
	}
	s.HasChanges, s.HasStashes = true, true
	if !s.Enabled(CmdStashSave) || !s.Enabled(CmdStashApply) || !s.Enabled(CmdStashDrop) {
		t.Fatal("stash commands are off with changes and stashes")
	}
	s.Merging = true
	if s.Enabled(CmdStashSave) {
		t.Fatal("saving a stash is on during a merge")
	}
}
