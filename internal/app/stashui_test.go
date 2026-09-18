package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/stash"
)

func toolbarMenuOnDispatcher(t *testing.T, a *App, name string) *widget.MenuButton {
	t.Helper()
	btn := readOnDispatcher(t, a, func() *widget.MenuButton { btn, _ := a.toolbarMenuButton(name); return btn })
	if btn == nil {
		t.Fatalf("toolbar menu button %q is missing", name)
	}
	return btn
}

type menuButtonState struct {
	Enabled bool
	Action  bool
	Menu    bool
}

func menuButtonStateOf(t *testing.T, a *App, btn *widget.MenuButton) menuButtonState {
	t.Helper()
	return readOnDispatcher(t, a, func() menuButtonState {
		return menuButtonState{Enabled: btn.IsEnabled(), Action: btn.ActionEnabled(), Menu: btn.MenuEnabled()}
	})
}

func openedMenuItems(t *testing.T, a *App, btn *widget.MenuButton) []widget.MenuItem {
	t.Helper()
	return readOnDispatcher(t, a, func() []widget.MenuItem { btn.OnOpening(); return btn.Items })
}

func waitForAppState(t *testing.T, a *App, what string, ready func(State) bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !ready(readOnDispatcher(t, a, a.State)) {
		if time.Now().After(deadline) {
			t.Fatalf("the state did not reach %s in time", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func selectWorkingFile(t *testing.T, a *App, path string) (changes.Row, int) {
	t.Helper()
	for i := range filesRowCountOnDispatcher(t, a) {
		row := filesRowOnDispatcher(t, a, i)
		if row.RelPath != path {
			continue
		}
		runOnDispatcher(t, a, func() {
			grid := a.filesGrid.Data().Grid
			grid.SetSelectedIndex(i)
			grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: i, SelectedItem: row})
		})
		return row, i
	}
	t.Fatalf("the files grid has no row %q", path)
	return changes.Row{}, -1
}

func dirtyAppWithUntrackedFile(t *testing.T) (*App, string) {
	t.Helper()
	a, target := blockedSwitchApp(t, "dirty\n")
	if err := writeFile(target, "new.txt", "fresh\n"); err != nil {
		t.Fatal(err)
	}
	runOnDispatcher(t, a, a.RefreshRepository)
	waitForFileRowStatus(t, a, "new.txt", changes.RowUntracked)
	waitForFileRowStatus(t, a, "f.txt", changes.RowModified)
	waitForAppState(t, a, "local changes", func(s State) bool { return s.HasStashable })
	return a, target
}

func TestTheToolbarPlacesTheStashButtonsBetweenCommitAndGitFlow(t *testing.T) {
	a := newTestApp(t)
	children := a.Widget("toolbar").(*widget.StackPanel).Children()
	at := func(name string) int { return slices.Index(children, a.Widget(name)) }

	commit, save, apply, flow := at("btnCommit"), at("btnSaveStash"), at("btnApplyStash"), at("btnGitFlow")
	inOrder := commit >= 0 && commit < save && save+1 == apply && apply < flow
	if !inOrder {
		t.Fatalf("toolbar order: commit %d, save %d, apply %d, git-flow %d", commit, save, apply, flow)
	}
	for _, name := range []string{"btnSaveStash", "btnApplyStash"} {
		btn := toolbarMenuOnDispatcher(t, a, name)
		if !btn.Split || btn.IsEnabled() || btn.Icon == nil {
			t.Fatalf("%s: split %v, enabled %v, icon %v", name, btn.Split, btn.IsEnabled(), btn.Icon != nil)
		}
	}
}

func TestTheApplyStashButtonListsTheStashesAndOpensTheDialog(t *testing.T) {
	a, _ := stashedApp(t)
	views := captureStashViews(t)
	btn := toolbarMenuOnDispatcher(t, a, "btnApplyStash")

	if got := menuButtonStateOf(t, a, btn); got != (menuButtonState{Enabled: true, Action: true, Menu: true}) {
		t.Fatalf("apply button with a stash = %+v", got)
	}
	items := openedMenuItems(t, a, btn)
	if len(items) != 1 || items[0].Text != "stash@{0}: On main: keep" || items[0].Disabled {
		t.Fatalf("apply menu = %+v", menuTexts(items))
	}
	runOnDispatcher(t, a, items[0].OnClick)
	runOnDispatcher(t, a, btn.OnClick)
	if len(*views) != 2 || readOnDispatcher(t, a, (*views)[0].Request).Index != 0 {
		t.Fatalf("apply dialogs = %d", len(*views))
	}

	runOnDispatcher(t, a, func() { a.dropStash(0) })
	waitForStatusText(t, a, i18n.Tf("Status.StashDropped", "stash@{0}"))
	if got := menuButtonStateOf(t, a, btn); got != (menuButtonState{Enabled: true}) {
		t.Fatalf("apply button without stashes = %+v", got)
	}
	if items := openedMenuItems(t, a, btn); len(items) != 0 {
		t.Fatalf("apply menu without stashes = %v", menuTexts(items))
	}
}

func TestTheApplyStashMenuIsEmptyWhenTheStashesCannotBeRead(t *testing.T) {
	a, _ := stashedApp(t)
	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prev })

	if items := readOnDispatcher(t, a, a.applyStashMenuItems); items != nil {
		t.Fatalf("items = %v", menuTexts(items))
	}
}

func TestTheSaveStashButtonFollowsTheChangesAndTheFileSelection(t *testing.T) {
	a, _ := stashedApp(t)
	btn := toolbarMenuOnDispatcher(t, a, "btnSaveStash")
	waitForAppState(t, a, "a clean working tree", func(s State) bool { return !s.HasStashable })
	if got := menuButtonStateOf(t, a, btn); got != (menuButtonState{Enabled: true, Menu: true}) {
		t.Fatalf("save button without changes = %+v", got)
	}
	clean := openedMenuItems(t, a, btn)
	want := []string{i18n.T("Menu.Local.SaveStash"), i18n.T("Menu.Files.StashSelection")}
	if !slices.Equal(menuTexts(clean), want) || !clean[0].Disabled || !clean[1].Disabled {
		t.Fatalf("save menu without changes = %+v", clean)
	}

	a, _ = dirtyAppWithUntrackedFile(t)
	btn = toolbarMenuOnDispatcher(t, a, "btnSaveStash")
	saves := captureSaveStashViews(t)
	pushed := capturePushedStashOptions(t, nil)
	if got := menuButtonStateOf(t, a, btn); got != (menuButtonState{Enabled: true, Action: true, Menu: true}) {
		t.Fatalf("save button with changes = %+v", got)
	}
	runOnDispatcher(t, a, btn.OnClick)
	dirty := openedMenuItems(t, a, btn)
	if dirty[0].Disabled || !dirty[1].Disabled {
		t.Fatalf("save menu with changes and no selection = %+v", dirty)
	}
	runOnDispatcher(t, a, dirty[0].OnClick)

	selectWorkingFile(t, a, "f.txt")
	selected := openedMenuItems(t, a, btn)
	if selected[1].Disabled {
		t.Fatal("stash selection is off with a selected working file")
	}
	runOnDispatcher(t, a, selected[1].OnClick)
	if len(*saves) != 3 || readOnDispatcher(t, a, (*saves)[2].Dialog).Title != i18n.T("Dialog.StashSelection.Title") {
		t.Fatalf("save dialogs = %d", len(*saves))
	}
	view := (*saves)[2]
	rebaseThrough(t, a, func() { view.OnOK(stash.SaveRequest{Message: "only f", KeepIndex: true}) })

	wantOpts := ops.StashOptions{Message: "only f", KeepIndex: true, Paths: []string{"f.txt"}}
	if len(*pushed) != 1 || !reflect.DeepEqual((*pushed)[0], wantOpts) {
		t.Fatalf("pushed = %+v, want %+v", *pushed, wantOpts)
	}
}

func TestStashSelectionOfAnUntrackedFileIncludesUntrackedFiles(t *testing.T) {
	a, _ := dirtyAppWithUntrackedFile(t)
	saves := captureSaveStashViews(t)
	pushed := capturePushedStashOptions(t, nil)

	row, index := selectWorkingFile(t, a, "new.txt")
	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.filesMenu(row, index) })
	item, found := findMenuItem(items, i18n.T("Menu.Files.StashSelection"))
	if !found || item.Disabled || item.Icon == nil {
		t.Fatalf("stash selection item = %+v", item)
	}
	runOnDispatcher(t, a, item.OnClick)
	if len(*saves) != 1 {
		t.Fatalf("save dialogs = %d", len(*saves))
	}
	view := (*saves)[0]
	rebaseThrough(t, a, func() { view.OnOK(stash.SaveRequest{Message: "new"}) })

	want := ops.StashOptions{Message: "new", IncludeUntracked: true, Paths: []string{"new.txt"}}
	if len(*pushed) != 1 || !reflect.DeepEqual((*pushed)[0], want) {
		t.Fatalf("pushed = %+v, want %+v", *pushed, want)
	}
}

func TestTheStashSelectionCommandFollowsTheSelectedFiles(t *testing.T) {
	s := State{ActiveRepository: "r"}
	if s.Enabled(CmdStashSelection) {
		t.Fatal("stash selection is on without selected files")
	}
	s.FilesSelected = true
	if !s.Enabled(CmdStashSelection) {
		t.Fatal("stash selection is off with selected files")
	}
	s.Merging = true
	if s.Enabled(CmdStashSelection) {
		t.Fatal("stash selection is on during a merge")
	}
}

func TestTheApplyDialogPassesTheRestoreIndexChoice(t *testing.T) {
	a, _ := stashedApp(t)
	views := captureStashViews(t)
	var applied, popped []ops.StashApplyOptions
	prevApply, prevPop := runStashApply, runStashPop
	runStashApply = func(_ context.Context, _ *gitrepo.Repository, _ int, opts ops.StashApplyOptions) (ops.StashApplyResult, error) {
		applied = append(applied, opts)
		return ops.StashApplyResult{}, nil
	}
	runStashPop = func(_ context.Context, _ *gitrepo.Repository, _ int, opts ops.StashApplyOptions) (ops.StashApplyResult, error) {
		popped = append(popped, opts)
		return ops.StashApplyResult{Dropped: true}, nil
	}
	t.Cleanup(func() { runStashApply, runStashPop = prevApply, prevPop })

	runOnDispatcher(t, a, func() { a.Dispatch(CmdStashApply) })
	view := (*views)[0]
	rebaseThrough(t, a, func() { view.OnOK(stash.Request{Index: 0, RestoreIndex: true}) })
	rebaseThrough(t, a, func() { view.OnOK(stash.Request{Index: 0, Drop: true, RestoreIndex: true}) })

	indexed := []ops.StashApplyOptions{{Index: true}}
	if !slices.Equal(applied, indexed) || !slices.Equal(popped, indexed) {
		t.Fatalf("applied = %+v, popped = %+v", applied, popped)
	}
}

func TestMissingToolbarMenuButtonsAreSkipped(t *testing.T) {
	a := newTestApp(t)
	for _, entry := range toolbarMenuButtons() {
		delete(a.named, entry.Name)
	}

	a.wireToolbarMenus()
	a.refreshToolbarMenus(State{ActiveRepository: "r"})
	a.applyToolbarIcons(nil)

	if got := len(a.toolbarCaptionWidths()); got != len(toolbarButtons) {
		t.Fatalf("caption widths = %d, want only the plain buttons", got)
	}
}

func stashedAppWithUntrackedFile(t *testing.T) (*App, string, hash.ObjectID) {
	t.Helper()
	a, target := blockedSwitchApp(t, "dirty\n")
	if err := writeFile(target, "new.txt", "fresh\n"); err != nil {
		t.Fatal(err)
	}
	r := openRepoAt(t, target)
	id, err := ops.StashPush(t.Context(), r, ops.StashOptions{Message: "both", IncludeUntracked: true})
	if err != nil {
		t.Fatal(err)
	}
	runOnDispatcher(t, a, a.RefreshRepository)
	waitForWorkingIdle(t, a)
	return a, target, id
}

func selectBranchNode(t *testing.T, a *App, ref refs.Name) {
	t.Helper()
	runOnDispatcher(t, a, func() {
		item, ok := a.branchesView.Item(ref)
		if !ok {
			t.Errorf("the branches pane has no %s", ref)
			return
		}
		a.Widget("branchesTree").(*widget.TreeViewWidget).Tree.SetSelectedItem(item)
	})
}

func branchSelectionOnDispatcher(t *testing.T, a *App) bool {
	t.Helper()
	return readOnDispatcher(t, a, func() bool {
		return a.Widget("branchesTree").(*widget.TreeViewWidget).Tree.SelectedItem() != nil
	})
}

func TestSelectingAStashShowsItsFilesAndDiffLikeACommit(t *testing.T) {
	a, _, id := stashedAppWithUntrackedFile(t)

	selectBranchNode(t, a, branches.StashRef(0))
	waitForFilesPaths(t, a, "f.txt", "new.txt")

	if doc := diffDocumentOnDispatcher(t, a); doc.NewName != "f.txt" || !strings.Contains(doc.Right, "dirty") {
		t.Fatalf("first diff = %+v", doc)
	}
	runOnDispatcher(t, a, func() {
		row, _ := a.filesItems.Get(1).(changes.Row)
		a.filesGrid.Data().Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 1, SelectedItem: row})
	})
	if doc := diffDocumentOnDispatcher(t, a); doc.NewName != "new.txt" || !strings.Contains(doc.Right, "fresh") {
		t.Fatalf("untracked diff = %+v", doc)
	}
	shown, ok := readOnDispatcher(t, a, func() hash.ObjectID { shown, _ := a.shownCommit(); return shown }), readOnDispatcher(t, a, a.commitIsSelected)
	if shown != id || !ok || readOnDispatcher(t, a, a.statusLabel.Text) != i18n.Tf("Status.StashSelected", "stash@{0}", "On main: both") {
		t.Fatalf("shown = %s, selected = %v, status = %q", shown, ok, readOnDispatcher(t, a, a.statusLabel.Text))
	}

	runOnDispatcher(t, a, a.leaveCommitView)
	waitForWorkingRows(t, a, 0)
	if readOnDispatcher(t, a, a.commitIsSelected) || branchSelectionOnDispatcher(t, a) {
		t.Fatal("the working tree view kept the stash selected")
	}

	selectBranchNode(t, a, branches.StashRef(0))
	waitForFilesRows(t, a, 2)
	waitForJournalRows(t, a, 1)
	runOnDispatcher(t, a, func() {
		row := a.Widget("journalGrid").(*widget.DataGridWidget).Grid.ItemsSource().Get(0)
		a.Widget("journalGrid").(*widget.DataGridWidget).Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: row})
	})
	if branchSelectionOnDispatcher(t, a) {
		t.Fatal("a journal commit kept the stash selected")
	}
}

func TestSelectingSomethingOtherThanAStashKeepsTheWorkingTree(t *testing.T) {
	a, _, _ := stashedAppWithUntrackedFile(t)
	cases := []struct {
		name  string
		run   func()
		setup func()
	}{
		{name: "a branch", run: func() { a.onBranchRefSelected(refs.BranchName("main")) }},
		{name: "a stash that is gone", run: func() { a.showStashChanges(9) }},
		{name: "unreadable stashes", run: func() { a.showStashChanges(0) }, setup: func() {
			prev := loadBranchSnapshot
			loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
			t.Cleanup(func() { loadBranchSnapshot = prev })
		}},
	}
	for _, c := range cases {
		if c.setup != nil {
			c.setup()
		}
		runOnDispatcher(t, a, c.run)
		if readOnDispatcher(t, a, a.commitIsSelected) {
			t.Fatalf("%s left the working tree view", c.name)
		}
	}
}

func TestAStashThatCannotBeShownLeavesTheFilesAlone(t *testing.T) {
	a, _, _ := stashedAppWithUntrackedFile(t)
	prev := runStashShow
	runStashShow = func(context.Context, *gitrepo.Repository, int, diff.Options) (ops.StashChanges, error) {
		return ops.StashChanges{}, errors.New("broken stash")
	}
	t.Cleanup(func() { runStashShow = prev })

	selectBranchNode(t, a, branches.StashRef(0))
	a.diffWG.Wait()
	waitForPostQueueDrain(t, a)

	if filesModeOnDispatcher(a) != filesModeWorking {
		t.Fatal("a stash that failed to load replaced the working files")
	}
}
