package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/ui/switchbranch"
	"github.com/oops1/gogit/internal/ui/switchchanges"
)

func captureSwitchChangesViews(t *testing.T) *[]*switchchanges.View {
	t.Helper()
	views := &[]*switchchanges.View{}
	prev := newSwitchChangesView
	newSwitchChangesView = func() (*switchchanges.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newSwitchChangesView = prev })
	return views
}

func blockedSwitchApp(t *testing.T, content string) (*App, string) {
	t.Helper()
	a, target := forkedApp(t, false)
	if err := writeFile(target, "f.txt", content); err != nil {
		t.Fatal(err)
	}
	return a, target
}

func useSwitchChanges(t *testing.T, a *App, mode string) {
	t.Helper()
	runOnDispatcher(t, a, func() { a.cfg.Git.SwitchChanges = mode })
}

func switchChangesSetting(t *testing.T, a *App) string {
	t.Helper()
	return readOnDispatcher(t, a, func() string { return a.cfg.Git.SwitchChanges })
}

func stashCount(t *testing.T, target string) int {
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
	return len(entries)
}

func readWorkingFile(t *testing.T, target, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(target, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func switchToFeature(a *App) func() {
	return func() { a.startSwitch(switchbranch.Choice{Source: "feature"}) }
}

func TestABlockedSwitchAsksAndStashesTheChanges(t *testing.T) {
	a, target := blockedSwitchApp(t, "dirty\n")
	views := captureSwitchChangesViews(t)

	rebaseThrough(t, a, switchToFeature(a))
	waitForPostQueueDrain(t, a)
	if len(*views) != 1 {
		t.Fatalf("the changes dialog opened %d times", len(*views))
	}
	view := (*views)[0]
	lines := rebaseThrough(t, a, func() {
		view.OnChoose(switchchanges.Choice{Mode: config.SwitchChangesStash, Remember: true})
	})

	if !logHasPrefix(t, lines, "Operation.Log.SwitchStashed") || !logHasPrefix(t, lines, "Operation.Log.Switched") {
		t.Fatalf("log = %v", lines)
	}
	if got := branchNow(t, a); got != "feature" {
		t.Fatalf("branch = %q", got)
	}
	if stashCount(t, target) != 1 {
		t.Fatal("the local changes were not stashed")
	}
	if got := switchChangesSetting(t, a); got != config.SwitchChangesStash {
		t.Fatalf("remembered setting = %q", got)
	}
}

func TestCancellingTheChangesDialogStaysOnTheBranch(t *testing.T) {
	a, target := blockedSwitchApp(t, "dirty\n")
	views := captureSwitchChangesViews(t)

	rebaseThrough(t, a, switchToFeature(a))
	waitForPostQueueDrain(t, a)
	view := (*views)[0]
	runOnDispatcher(t, a, func() { view.Dialog().CancelAction() })

	if got := branchNow(t, a); got != "main" {
		t.Fatalf("branch = %q", got)
	}
	if readWorkingFile(t, target, "f.txt") != "dirty\n" || switchChangesSetting(t, a) != config.SwitchChangesAsk {
		t.Fatal("cancelling touched the changes or the setting")
	}
}

func TestARememberedMergeCarriesTheChangesWithoutAsking(t *testing.T) {
	local := withLine(withLine(mergeLines("f"), 0, "OURS"), 4, "LOCAL")
	a, target := blockedSwitchApp(t, local)
	useSwitchChanges(t, a, config.SwitchChangesMerge)
	views := captureSwitchChangesViews(t)

	lines := rebaseThrough(t, a, switchToFeature(a))

	if len(*views) != 0 || !logHasPrefix(t, lines, "Operation.Log.SwitchCarried") {
		t.Fatalf("dialogs = %d, log = %v", len(*views), lines)
	}
	if got := branchNow(t, a); got != "feature" {
		t.Fatalf("branch = %q", got)
	}
	if want := withLine(mergeLines("f"), 4, "LOCAL"); readWorkingFile(t, target, "f.txt") != want {
		t.Fatalf("f.txt = %q", readWorkingFile(t, target, "f.txt"))
	}
}

func TestARememberedMergeReportsConflictsAndKeepsTheStash(t *testing.T) {
	a, target := blockedSwitchApp(t, "dirty\n")
	useSwitchChanges(t, a, config.SwitchChangesMerge)

	lines := rebaseThrough(t, a, switchToFeature(a))

	if !logHasPrefix(t, lines, "Operation.Log.SwitchCarryConflicts") || stashCount(t, target) != 1 {
		t.Fatalf("log = %v", lines)
	}
}

func TestARememberedOverwriteDiscardsTheChanges(t *testing.T) {
	a, target := blockedSwitchApp(t, "dirty\n")
	useSwitchChanges(t, a, config.SwitchChangesOverwrite)

	lines := rebaseThrough(t, a, switchToFeature(a))

	if !logHasPrefix(t, lines, "Operation.Log.SwitchOverwritten") || branchNow(t, a) != "feature" {
		t.Fatalf("log = %v", lines)
	}
	if readWorkingFile(t, target, "f.txt") == "dirty\n" {
		t.Fatal("the local change survived the overwrite")
	}
}

func TestTheChangesDialogFailureIsLogged(t *testing.T) {
	a, _ := blockedSwitchApp(t, "dirty\n")
	prev := newSwitchChangesView
	newSwitchChangesView = func() (*switchchanges.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newSwitchChangesView = prev })

	rebaseThrough(t, a, switchToFeature(a))
	waitForPostQueueDrain(t, a)

	if got := branchNow(t, a); got != "main" {
		t.Fatalf("branch = %q", got)
	}
}

func TestResolvingABlockedSwitchReportsFailures(t *testing.T) {
	a, _ := blockedSwitchApp(t, "dirty\n")
	prevPush, prevMerge, prevSwitch := runStashPush, runSwitchMerging, runSwitchBranch
	runStashPush = func(context.Context, *gitrepo.Repository, ops.StashOptions) (hash.ObjectID, error) {
		return hash.Zero, errors.New("no stash")
	}
	runSwitchMerging = func(context.Context, *gitrepo.Repository, string) (ops.SwitchMergeResult, error) {
		return ops.SwitchMergeResult{}, errors.New("no merge")
	}
	runSwitchBranch = func(context.Context, *gitrepo.Repository, string, ops.SwitchOptions) error {
		return errors.New("no switch")
	}
	t.Cleanup(func() { runStashPush, runSwitchMerging, runSwitchBranch = prevPush, prevMerge, prevSwitch })

	for _, mode := range []string{config.SwitchChangesStash, config.SwitchChangesMerge, config.SwitchChangesOverwrite} {
		lines := rebaseThrough(t, a, func() { a.switchCarryingChanges("feature", mode, []string{"f.txt"}) })
		if logHasPrefix(t, lines, "Operation.Log.Switched") {
			t.Fatalf("%s reported a switch: %v", mode, lines)
		}
	}

	prevOpen := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prevOpen })
	rebaseThrough(t, a, func() { a.switchCarryingChanges("feature", config.SwitchChangesStash, nil) })

	if got := branchNow(t, a); got != "main" {
		t.Fatalf("branch = %q", got)
	}
}

func TestRememberingAChoiceSurvivesAnUnsavableConfig(t *testing.T) {
	a, _ := blockedSwitchApp(t, "dirty\n")
	path := readOnDispatcher(t, a, func() string { return a.paths.ConfigFile() })
	_ = os.Remove(path)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	runOnDispatcher(t, a, func() { a.rememberSwitchChanges(config.SwitchChangesMerge) })

	if got := switchChangesSetting(t, a); got != config.SwitchChangesMerge {
		t.Fatalf("setting = %q", got)
	}
}

func TestSwitchCarryingChangesNeedsARepository(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)

	a.switchCarryingChanges("feature", config.SwitchChangesStash, nil)

	if len(*views) != 0 {
		t.Fatal("an operation started without a repository")
	}
}
