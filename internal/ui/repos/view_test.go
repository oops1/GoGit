package repos

import (
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/repo"
)

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
	v.Render(reg)

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
	if feature.DisplayText() != "⎇ feature" {
		t.Fatalf("feature text = %q", feature.DisplayText())
	}
}

func TestRenderMarksActiveNodeWithPrefix(t *testing.T) {
	reg, _ := newTestRegistry(t)
	if err := reg.SetActive(workGroupID(t, reg)); err != nil {
		t.Fatal(err)
	}
	v, tw := bound(t)
	v.Render(reg)
	roots := tw.Tree.Roots()
	var found *treeview.TreeViewItem
	for _, r := range roots {
		if r.DisplayText() == "● Work" {
			found = r
		}
	}
	if found == nil {
		t.Fatal("active group must be prefixed")
	}
}

func workGroupID(t *testing.T, reg *repo.Registry) string {
	t.Helper()
	for n := range reg.Walk() {
		if n.Kind == repo.KindGroup && n.Name == "Work" {
			return n.ID
		}
	}
	t.Fatal("work group not found")
	return ""
}

func TestRenderMarksActiveWorktreeWithBothPrefixes(t *testing.T) {
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
	if err := reg.SetActive(featureID); err != nil {
		t.Fatal(err)
	}
	v, tw := bound(t)
	v.Render(reg)
	item, ok := v.Item(featureID)
	if !ok {
		t.Fatal("item not tracked")
	}
	if item.DisplayText() != "● ⎇ feature" {
		t.Fatalf("text = %q", item.DisplayText())
	}
	_ = tw
}

func TestRenderDefaultsGroupsToExpanded(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg)
	for _, r := range tw.Tree.Roots() {
		if !r.Expanded {
			t.Fatalf("group %q must default to expanded", r.DisplayText())
		}
	}
}

func TestRenderPreservesCollapsedStateAcrossRenders(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg)

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

	v.Render(reg)
	for _, r := range tw.Tree.Roots() {
		if r.DisplayText() == "Work" && r.Expanded {
			t.Fatal("collapsed state must survive re-render")
		}
	}
}

func TestRenderKeepsNewGroupsExpandedAfterPartialCollapse(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg)

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
	v.Render(reg)

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
	v.Render(reg)

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
	v.Render(reg)

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
	v.Render(reg)
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
	v.Render(reg)

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
	v.Render(reg)
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
	v.Render(reg)
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
	v.captureExpanded()
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
	v.Render(reg)
	if len(tw.Tree.Roots()) != 0 {
		t.Fatalf("roots = %d, want 0", len(tw.Tree.Roots()))
	}
}
