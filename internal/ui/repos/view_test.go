package repos

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/icons"
)

func TestMain(m *testing.M) {
	cat, err := i18n.Builtin()
	if err != nil {
		panic(err)
	}
	cat.Register()
	m.Run()
}

func newTestRegistry(t *testing.T) (*repo.Registry, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	reg := repo.New(cfg)
	work, err := reg.AddGroup("Work", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddGroup("archive", ""); err != nil {
		t.Fatal(err)
	}
	main, err := reg.AddRepository("Main", filepath.Join(dir, "main"), work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddWorktree(main.ID, "feature", filepath.Join(dir, "feature")); err != nil {
		t.Fatal(err)
	}
	return reg, cfg
}

func bound(t *testing.T) (*View, *widget.TreeViewWidget) {
	t.Helper()
	v := NewView()
	tw := widget.NewTreeViewWidget()
	v.Bind(tw)
	return v, tw
}

func TestRenderBuildsTreeStructureMatchingRegistry(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	roots := tw.Tree.Roots()
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	if roots[0].DisplayText() != "archive" || roots[1].DisplayText() != "Work" {
		t.Fatalf("root order = %q, %q", roots[0].DisplayText(), roots[1].DisplayText())
	}
	work := roots[1]
	if len(work.Children) != 1 {
		t.Fatalf("work children = %d", len(work.Children))
	}
	main := work.Children[0]
	if main.DisplayText() != "Main" {
		t.Fatalf("main text = %q", main.DisplayText())
	}
	if len(main.Children) != 1 {
		t.Fatalf("main children = %d", len(main.Children))
	}
	feature := main.Children[0]
	if feature.DisplayText() != "feature" {
		t.Fatalf("feature text = %q", feature.DisplayText())
	}
}

func TestRenderAssignsAnIconToEveryNode(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	var walk func(items []*treeview.TreeViewItem)
	walk = func(items []*treeview.TreeViewItem) {
		for _, item := range items {
			if item.Icon == nil {
				t.Fatalf("node %q has no icon", item.DisplayText())
			}
			walk(item.Children)
		}
	}
	walk(tw.Tree.Roots())
}

func TestRenderUsesTheOpenGroupIconForAnExpandedGroupAndTheClosedIconOtherwise(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	var work *treeview.TreeViewItem
	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "Work" {
			work = r
		}
	}
	if work == nil {
		t.Fatal("work group missing")
	}
	openIcon := work.Icon
	if openIcon == nil {
		t.Fatal("expanded group must have an icon")
	}
	tw.Tree.CollapseItem(work)
	v.Render(reg, nil)

	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "Work" {
			work = r
		}
	}
	if work.Icon == openIcon {
		t.Fatal("a collapsed group must use a different icon than an expanded one")
	}
}

func TestRenderGivesARepositoryTheModifiedIconWhenMarked(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	v, tw := bound(t)
	v.Render(reg, map[string]State{mainID: {Modified: true}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	_ = tw
	if item.Icon == icons.Tree("repository", treeIconSize) {
		t.Fatal("a modified repository must not use the plain repository icon")
	}
	if item.Icon != icons.Tree("repository_modified", treeIconSize) {
		t.Fatal("a modified repository must use the repository_modified icon")
	}
}

func TestRenderGivesARepositoryTheMissingIconWhenMarked(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {Missing: true, Modified: true}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.Icon != icons.Tree("repository_missing", treeIconSize) {
		t.Fatal("missing must take priority over modified and use the repository_missing icon")
	}
}

func TestRenderUsesThePlainRepositoryIconWithoutState(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	v, _ := bound(t)
	v.Render(reg, nil)

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.Icon != icons.Tree("repository", treeIconSize) {
		t.Fatal("a repository without state must use the plain repository icon")
	}
}

func TestRenderTintsTheActiveRepositoryIconWithAccent(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	if err := reg.SetActive(mainID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	accent := color.RGBA{R: 76, G: 194, B: 255, A: 255}
	v.SetAccent(accent)
	v.Render(reg, nil)

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.Icon == icons.Tree("repository", treeIconSize) {
		t.Fatal("the active repository must not use the plain, untinted repository icon")
	}
	want := icons.TreeTinted("repository", treeIconSize, accent)
	if item.Icon != want {
		t.Fatal("the active repository must use the accent-tinted repository icon")
	}
}

func TestRenderDoesNotTintInactiveRepositories(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var mainID, worktreeID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
		if n.Kind == repo.KindWorktree {
			worktreeID = n.ID
		}
	}
	if err := reg.SetActive(worktreeID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	v.SetAccent(color.RGBA{R: 76, G: 194, B: 255, A: 255})
	v.Render(reg, nil)

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.Icon != icons.Tree("repository", treeIconSize) {
		t.Fatal("an inactive repository must keep the plain, untinted icon")
	}
}

func TestRenderTintsTheActiveWorktreeIconWithAccent(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var worktreeID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindWorktree {
			worktreeID = n.ID
		}
	}
	if err := reg.SetActive(worktreeID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	accent := color.RGBA{R: 76, G: 194, B: 255, A: 255}
	v.SetAccent(accent)
	v.Render(reg, nil)

	item, ok := v.Item(worktreeID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.Icon != icons.TreeTinted("worktree", treeIconSize, accent) {
		t.Fatal("the active worktree must use the accent-tinted worktree icon")
	}
}

func TestRenderGivesAWorktreeItsOwnIcon(t *testing.T) {
	reg, _ := newTestRegistry(t)
	var featureID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindWorktree {
			featureID = n.ID
		}
	}
	if featureID == "" {
		t.Fatal("worktree not found")
	}
	v, _ := bound(t)
	v.Render(reg, nil)
	item, ok := v.Item(featureID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.DisplayText() != "feature" {
		t.Fatalf("text = %q", item.DisplayText())
	}
	if item.Icon == nil {
		t.Fatal("worktree must have an icon")
	}
}

func TestRenderDefaultsGroupsToExpanded(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	for _, r := range tw.Tree.Roots() {
		if !r.Expanded {
			t.Fatalf("group %q must default to expanded", r.DisplayText())
		}
	}
}

func TestRenderPreservesCollapsedStateAcrossRenders(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	var work *treeview.TreeViewItem
	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "Work" {
			work = r
		}
	}
	if work == nil {
		t.Fatal("work group missing")
	}
	tw.Tree.CollapseItem(work)

	v.Render(reg, nil)
	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "Work" && r.Expanded {
			t.Fatal("collapsed state must survive re-render")
		}
	}
}

func TestRenderKeepsNewGroupsExpandedAfterPartialCollapse(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	var work *treeview.TreeViewItem
	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "Work" {
			work = r
		}
	}
	tw.Tree.CollapseItem(work)

	if _, err := reg.AddGroup("NewGroup", ""); err != nil {
		t.Fatal(err)
	}
	v.Render(reg, nil)

	found := false
	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "NewGroup" {
			found = true
			if !r.Expanded {
				t.Fatal("new group must default to expanded")
			}
		}
		if r.DisplayText() == "Work" && r.Expanded {
			t.Fatal("work must still be collapsed")
		}
	}
	if !found {
		t.Fatal("new group missing from tree")
	}
}

func TestOnActivateFiresOnItemInvoked(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, _ := bound(t)
	v.Render(reg, nil)

	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item missing")
	}

	invoked := ""
	v.OnActivate = func(id string) { invoked = id }
	v.tree.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
	if invoked != mainID {
		t.Fatalf("invoked = %q, want %q", invoked, mainID)
	}
}

func TestOnActivateIgnoresUnknownItem(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, _ := bound(t)
	v.Render(reg, nil)

	called := false
	v.OnActivate = func(string) { called = true }
	v.tree.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: treeview.NewItem("ghost")})
	if called {
		t.Fatal("unknown item must not trigger OnActivate")
	}
}

func TestOnActivateNotSetDoesNotPanic(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, _ := bound(t)
	v.Render(reg, nil)
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	item, _ := v.Item(mainID)
	v.tree.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
}

func TestOnSelectFiresOnSelectedItemChanged(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item missing")
	}

	selected := ""
	v.OnSelect = func(id string) { selected = id }
	tw.Tree.SetSelectedItem(item)
	if selected != mainID {
		t.Fatalf("selected = %q, want %q", selected, mainID)
	}
}

func TestOnSelectIgnoresNilSelection(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	item, _ := v.Item(mainID)
	tw.Tree.SetSelectedItem(item)

	called := false
	v.OnSelect = func(string) { called = true }
	tw.Tree.SetSelectedItem(nil)
	if called {
		t.Fatal("nil selection must not trigger OnSelect")
	}
}

func TestOnSelectIgnoresUnknownItemAndMissingHandler(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	tw.Tree.SetSelectedItem(treeview.NewItem("ghost"))

	called := false
	v.OnSelect = func(string) { called = true }
	tw.Tree.SetSelectedItem(treeview.NewItem("ghost2"))
	if called {
		t.Fatal("unknown item must not trigger OnSelect")
	}

	v.OnSelect = nil
	var mainID string
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			mainID = n.ID
		}
	}
	item, _ := v.Item(mainID)
	tw.Tree.SetSelectedItem(item)
}

func TestCaptureExpandedIsNoOpBeforeBind(t *testing.T) {
	v := NewView()
	v.mu.Lock()
	v.captureExpandedLocked()
	v.mu.Unlock()
}

func TestItemReturnsFalseForUnknownID(t *testing.T) {
	v := NewView()
	if _, ok := v.Item("missing"); ok {
		t.Fatal("expected not found")
	}
}

func TestRenderOnEmptyRegistryProducesNoRoots(t *testing.T) {
	cfg := config.Default()
	reg := repo.New(cfg)
	v, tw := bound(t)
	v.Render(reg, nil)
	if len(tw.Tree.Roots()) != 0 {
		t.Fatalf("roots = %d, want 0", len(tw.Tree.Roots()))
	}
}

func findRepositoryID(t *testing.T, reg *repo.Registry) string {
	t.Helper()
	for n := range reg.Walk() {
		if n.Kind == repo.KindRepository {
			return n.ID
		}
	}
	t.Fatal("no repository node found")
	return ""
}

func TestRenderShowsAheadBadgeOnlyForTheActiveRepository(t *testing.T) {
	reg, _ := newTestRegistry(t)
	mainID := findRepositoryID(t, reg)
	if err := reg.SetActive(mainID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {HasUpstream: true, Ahead: 2}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	want := "Main " + i18n.Tf("Status.Ahead", 2)
	if item.DisplayText() != want {
		t.Fatalf("text = %q, want %q", item.DisplayText(), want)
	}
}

func TestRenderShowsBehindBadgeForTheActiveRepository(t *testing.T) {
	reg, _ := newTestRegistry(t)
	mainID := findRepositoryID(t, reg)
	if err := reg.SetActive(mainID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {HasUpstream: true, Behind: 3}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	want := "Main " + i18n.Tf("Status.Behind", 3)
	if item.DisplayText() != want {
		t.Fatalf("text = %q, want %q", item.DisplayText(), want)
	}
}

func TestRenderShowsDivergedBadgeForTheActiveRepository(t *testing.T) {
	reg, _ := newTestRegistry(t)
	mainID := findRepositoryID(t, reg)
	if err := reg.SetActive(mainID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {HasUpstream: true, Ahead: 1, Behind: 2}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	want := "Main " + i18n.Tf("Status.Diverged", 1, 2)
	if item.DisplayText() != want {
		t.Fatalf("text = %q, want %q", item.DisplayText(), want)
	}
}

func TestRenderOmitsTheBadgeWithoutAnUpstream(t *testing.T) {
	reg, _ := newTestRegistry(t)
	mainID := findRepositoryID(t, reg)
	if err := reg.SetActive(mainID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {Ahead: 2, Behind: 3}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.DisplayText() != "Main" {
		t.Fatalf("text = %q, want %q", item.DisplayText(), "Main")
	}
}

func TestRenderOmitsTheBadgeWhenUpToDate(t *testing.T) {
	reg, _ := newTestRegistry(t)
	mainID := findRepositoryID(t, reg)
	if err := reg.SetActive(mainID); err != nil {
		t.Fatal(err)
	}
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {HasUpstream: true}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.DisplayText() != "Main" {
		t.Fatalf("text = %q, want %q", item.DisplayText(), "Main")
	}
}

func TestRenderOmitsTheBadgeForAnInactiveRepositoryEvenWithDivergence(t *testing.T) {
	reg, _ := newTestRegistry(t)
	mainID := findRepositoryID(t, reg)
	v, _ := bound(t)
	v.Render(reg, map[string]State{mainID: {HasUpstream: true, Ahead: 2, Behind: 1}})

	item, ok := v.Item(mainID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.DisplayText() != "Main" {
		t.Fatalf("text = %q, want %q", item.DisplayText(), "Main")
	}
}

func nodeItem(t *testing.T, v *View, reg *repo.Registry, name string) *treeview.TreeViewItem {
	t.Helper()
	for node := range reg.Walk() {
		if node.Name != name {
			continue
		}
		item, ok := v.Item(node.ID)
		if !ok {
			t.Fatalf("node %q has no item", name)
		}
		return item
	}
	t.Fatalf("node %q is not in the registry", name)
	return nil
}

func TestTheTreeLetsTheUserDragNodes(t *testing.T) {
	_, tw := bound(t)

	if !tw.Tree.CanUserDragNodes {
		t.Fatal("repositories and groups are arranged by dragging them")
	}
}

func TestARepositoryGoesIntoAGroupAndNextToAnotherNode(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	main := nodeItem(t, v, reg, "Main")
	archive := nodeItem(t, v, reg, "archive")
	work := nodeItem(t, v, reg, "Work")

	if !tw.Tree.CanDropNode(main, archive, treeview.DropInside) {
		t.Fatal("a repository must go into a group")
	}
	if !tw.Tree.CanDropNode(main, work, treeview.DropAfter) {
		t.Fatal("a repository must go next to a group")
	}
}

func TestAWorktreeAndADirectoryStayWhereTheyAre(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	feature := nodeItem(t, v, reg, "feature")
	archive := nodeItem(t, v, reg, "archive")
	main := nodeItem(t, v, reg, "Main")

	if tw.Tree.CanDropNode(feature, archive, treeview.DropInside) {
		t.Fatal("a worktree belongs to its repository, not to a group")
	}
	if tw.Tree.CanDropNode(newStub(), archive, treeview.DropInside) {
		t.Fatal("only tree nodes are dragged")
	}
	if tw.Tree.CanDropNode(main, newStub(), treeview.DropInside) {
		t.Fatal("only tree nodes are drop targets")
	}
	if tw.Tree.CanDropNode(main, main, treeview.DropInside) {
		t.Fatal("a repository is not a group and holds nothing")
	}
	if tw.Tree.CanDropNode(main, feature, treeview.DropAfter) {
		t.Fatal("a repository cannot stand next to a worktree")
	}
}

func TestADroppedNodeIsReportedWithItsTarget(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	main := nodeItem(t, v, reg, "Main")
	archive := nodeItem(t, v, reg, "archive")
	var moved [3]string

	v.OnMove = func(id, targetID string, inside bool) {
		moved[0], moved[1] = id, targetID
		if inside {
			moved[2] = "inside"
		}
	}
	if !tw.Tree.OnNodeDrop(main, archive, treeview.DropInside) {
		t.Fatal("the view arranges the tree from the model, not by moving items")
	}

	mainID, _ := v.Item(moved[0])
	if mainID != main || moved[2] != "inside" {
		t.Fatalf("move = %v, want the repository inside the group", moved)
	}
	archiveItem, _ := v.Item(moved[1])
	if archiveItem != archive {
		t.Fatalf("target = %q, want the group", moved[1])
	}
}

func TestADropWithoutAListenerIsHarmless(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)

	if !tw.Tree.OnNodeDrop(nodeItem(t, v, reg, "Main"), nodeItem(t, v, reg, "archive"), treeview.DropInside) {
		t.Fatal("the drop must still be taken as handled")
	}
}

func TestACollapsedGroupIsReportedAndKept(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	work := nodeItem(t, v, reg, "Work")
	var reported []string
	v.OnGroupToggled = func(id string, expanded bool) {
		if expanded {
			reported = append(reported, "open:"+id)
			return
		}
		reported = append(reported, "shut:"+id)
	}

	tw.Tree.CollapseItem(work)
	v.Render(reg, nil)

	if len(reported) != 1 || reported[0][:5] != "shut:" {
		t.Fatalf("reported = %v, want the collapsed group", reported)
	}
	if nodeItem(t, v, reg, "Work").Expanded {
		t.Fatal("a collapsed group must stay collapsed across renders")
	}

	tw.Tree.ExpandItem(nodeItem(t, v, reg, "Work"))
	v.Render(reg, nil)

	if len(reported) != 2 || reported[1][:5] != "open:" {
		t.Fatalf("reported = %v, want the group opened again", reported)
	}
	if !nodeItem(t, v, reg, "Work").Expanded {
		t.Fatal("an expanded group must stay expanded across renders")
	}
}

func TestOnlyGroupsAreReportedAsCollapsed(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	reported := 0
	v.OnGroupToggled = func(string, bool) { reported++ }

	tw.Tree.OnCollapsed(treeview.CollapsedEvent{Item: nodeItem(t, v, reg, "Main")})
	tw.Tree.OnCollapsed(treeview.CollapsedEvent{Item: newStub()})
	tw.Tree.OnExpanded(treeview.ExpandedEvent{Item: newStub()})

	if reported != 0 {
		t.Fatalf("reported = %d, want only groups to be reported", reported)
	}
}

func TestGroupsCollapsedInAnEarlierRunStayCollapsed(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, _ := bound(t)
	work, ok := groupID(reg, "Work")
	if !ok {
		t.Fatal("the group is not in the registry")
	}

	v.SetCollapsedGroups([]string{work})
	v.Render(reg, nil)

	if nodeItem(t, v, reg, "Work").Expanded {
		t.Fatal("a group collapsed in an earlier run must open collapsed")
	}
}

func groupID(reg *repo.Registry, name string) (string, bool) {
	for node := range reg.Walk() {
		if node.Kind == repo.KindGroup && node.Name == name {
			return node.ID, true
		}
	}
	return "", false
}

func TestDraggingWithTheMouseMovesARepositoryIntoAGroup(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	tw.Tree.ItemHeight = 20
	tw.SetBounds(image.Rect(0, 0, 240, 400))
	v.Render(reg, nil)
	var movedID, targetID string
	inside := false
	v.OnMove = func(id, target string, in bool) { movedID, targetID, inside = id, target, in }

	rowY := func(item *treeview.TreeViewItem) int {
		for i, row := range visibleRows(tw) {
			if row == item {
				return i*20 + 10
			}
		}
		t.Fatalf("item %q is not on screen", item.DisplayText())
		return 0
	}
	main := nodeItem(t, v, reg, "Main")
	archive := nodeItem(t, v, reg, "archive")

	tw.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true, X: 60, Y: rowY(main)})
	tw.OnMouseMove(60, rowY(archive))
	tw.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false, X: 60, Y: rowY(archive)})

	mainID, _ := v.Item(movedID)
	archiveItem, _ := v.Item(targetID)
	if mainID != main || archiveItem != archive || !inside {
		t.Fatalf("moved %q onto %q inside=%v, want the repository into the group", movedID, targetID, inside)
	}
}

func visibleRows(tw *widget.TreeViewWidget) []*treeview.TreeViewItem {
	var rows []*treeview.TreeViewItem
	var walk func(items []*treeview.TreeViewItem)
	walk = func(items []*treeview.TreeViewItem) {
		for _, item := range items {
			rows = append(rows, item)
			if item.Expanded {
				walk(item.Children)
			}
		}
	}
	walk(tw.Tree.Roots())
	return rows
}
