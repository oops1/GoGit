package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
)

func branchMenuOf(t *testing.T, a *App, ref refs.Name) []widget.MenuItem {
	t.Helper()
	return readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(ref) })
}

func refKeys(keys ...string) []string {
	texts := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "-" {
			texts = append(texts, key)
			continue
		}
		texts = append(texts, i18n.T(key))
	}
	return texts
}

func TestTheBranchAndTagMenusFollowSmartGit(t *testing.T) {
	a, _ := forkedApp(t, false)

	for _, c := range []struct {
		ref  refs.Name
		want []string
	}{
		{refs.BranchName("feature"), refKeys(
			"Menu.Ref.CheckOut", "-", "Menu.Ref.Merge", "Menu.Ref.RebaseOnto", "-", "Menu.Ref.Push", "Menu.Ref.PushTo", "-",
			"Menu.Ref.Log", "Menu.Ref.Rename", "-", "Menu.Ref.Reset", "Menu.Ref.ResetAdvanced", "Menu.Ref.Delete", "-",
			"Menu.Ref.SetTracked", "Menu.Ref.StopTracking", "-", "Menu.Ref.Copy", "-", "Menu.Ref.FormatPatch", "Menu.Ref.FastForward", "-",
			"Menu.Context.CompareWithCurrent", "Menu.Context.Reflog")},
		{"refs/remotes/origin/main", refKeys(
			"Menu.Ref.CheckOut", "-", "Menu.Ref.Merge", "Menu.Ref.RebaseOnto", "-", "Menu.Ref.Push", "Menu.Ref.PushTo", "-",
			"Menu.Ref.Log", "-", "Menu.Ref.Reset", "Menu.Ref.ResetAdvanced", "Menu.Ref.Delete", "-",
			"Menu.Ref.Copy", "-", "Menu.Ref.FormatPatch", "Menu.Ref.FastForward", "-", "Menu.Context.CompareWithCurrent")},
		{refs.TagName("v1"), refKeys(
			"Menu.Ref.CheckOut", "-", "Menu.Ref.Merge", "-", "Menu.Ref.Push", "Menu.Ref.PushTo", "-",
			"Menu.Ref.Log", "-", "Menu.Ref.Reset", "Menu.Ref.ResetAdvanced", "Menu.Ref.Delete", "-",
			"Menu.Ref.Copy", "Menu.Ref.CopyMessage", "-", "Menu.Ref.FormatPatch", "Menu.Ref.FastForward")},
	} {
		if got := menuTexts(branchMenuOf(t, a, c.ref)); !slices.Equal(got, c.want) {
			t.Errorf("%s menu = %v\nwant %v", c.ref, got, c.want)
		}
	}
	remote := branchMenuOf(t, a, "refs/remotes/origin/main")
	for _, key := range []string{"Menu.Ref.Push", "Menu.Ref.Delete", "Menu.Ref.PushTo", "Menu.Ref.FastForward"} {
		if item, _ := findMenuItem(remote, i18n.T(key)); !item.Disabled {
			t.Errorf("%s must be disabled on a remote branch", key)
		}
	}
}

func TestTheCurrentFlowBranchOffersItsGitFlowActions(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	integrations := captureFlowViews(t, &newFlowIntegrateView)
	runOnDispatcher(t, a, func() { a.setFlowState(flowOn(featureBranch, FlowBranch{})) })

	items := branchMenuOf(t, a, refs.BranchName("main"))
	integrate, found := findMenuItem(items, i18n.T("Menu.Tools.GitFlow.IntegrateDevelop"))
	if !found || integrate.Disabled || hasMenuItem(items, i18n.T("Menu.Ref.Merge")) {
		t.Fatalf("menu = %v", menuTexts(items))
	}
	runOnDispatcher(t, a, integrate.OnClick)

	waitForFlowView(t, a, integrations, 1)
}

func TestTheBranchMenuCopiesResetsAndPushes(t *testing.T) {
	a, _ := forkedApp(t, false)
	clipboard := captureClipboard(t)
	resets := captureResetViews(t)
	switches := captureSwitchViews(t)

	items := branchMenuOf(t, a, refs.BranchName("feature"))
	for _, key := range []string{"Menu.Ref.Copy", "Menu.Ref.Reset", "Menu.Ref.RebaseOnto"} {
		item, _ := findMenuItem(items, i18n.T(key))
		runOnDispatcher(t, a, item.OnClick)
	}
	checkOut, _ := findMenuItem(branchMenuOf(t, a, "refs/remotes/origin/main"), i18n.T("Menu.Ref.CheckOut"))
	runOnDispatcher(t, a, checkOut.OnClick)
	if len(*switches) != 1 {
		t.Fatalf("switch dialogs = %d", len(*switches))
	}
	runOnDispatcher(t, a, func() { a.resetToRef(refs.BranchName("missing")) })
	push, _ := findMenuItem(branchMenuOf(t, a, refs.BranchName("main")), i18n.T("Menu.Ref.Push"))
	runOnDispatcher(t, a, push.OnClick)
	newTestApp(t).resetToRef(refs.BranchName("main"))

	if *clipboard != "feature" || len(*resets) != 1 {
		t.Fatalf("clipboard = %q, reset dialogs = %d", *clipboard, len(*resets))
	}
}

func TestDeletingABranchOrATagAsksFirst(t *testing.T) {
	a, _ := forkedApp(t, false)
	answer := false
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(answer) }
	prev := runDeleteBranch
	t.Cleanup(func() { runDeleteBranch = prev })
	var deleted []string
	runDeleteBranch = func(_ context.Context, _ *gitrepo.Repository, name string, _ bool) error {
		deleted = append(deleted, name)
		return errors.New("not merged")
	}
	remove, _ := findMenuItem(branchMenuOf(t, a, refs.BranchName("feature")), i18n.T("Menu.Ref.Delete"))

	runOnDispatcher(t, a, remove.OnClick)
	runOnDispatcher(t, a, func() { a.confirmDeleteTag("v1") })
	answer = true
	runOnDispatcher(t, a, remove.OnClick)
	waitForStatusText(t, a, i18n.Tf("Status.BranchDeleteFailed", errors.New("not merged")))
	runDeleteBranch = func(_ context.Context, _ *gitrepo.Repository, name string, _ bool) error {
		deleted = append(deleted, name)
		return nil
	}
	runOnDispatcher(t, a, remove.OnClick)
	waitForStatusText(t, a, i18n.Tf("Status.BranchDeleted", "feature"))

	if !slices.Equal(deleted, []string{"feature", "feature"}) {
		t.Fatalf("deleted = %v", deleted)
	}
	if item, _ := findMenuItem(branchMenuOf(t, a, refs.BranchName("main")), i18n.T("Menu.Ref.Delete")); !item.Disabled {
		t.Fatal("the current branch cannot be deleted")
	}
}
