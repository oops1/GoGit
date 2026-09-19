package branches

import (
	"image"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestRenderKeepsTheSelectedBranchWithoutReportingANewSelection(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	selections := 0
	v.OnSelect = func(refs.Name) { selections++ }
	item, ok := v.Item(refs.BranchName("main"))
	if !ok {
		t.Fatal("main is not in the tree")
	}
	tw.Tree.SetSelectedItem(item)

	v.Render(fullSnapshot(t))

	again, _ := v.Item(refs.BranchName("main"))
	if tw.Tree.SelectedItem() != again || again == item {
		t.Fatalf("selected = %v, want the re-rendered main", tw.Tree.SelectedItem())
	}
	if selections != 1 {
		t.Fatalf("OnSelect ran %d times, want only the user's selection", selections)
	}
	if tw.Tree.OnSelectedItemChanged == nil {
		t.Fatal("the selection handler was not put back")
	}

	snap := fullSnapshot(t)
	snap.Local = snap.Local[:1]
	v.Render(snap)
	if tw.Tree.SelectedItem() != nil {
		t.Fatal("a branch that disappeared stayed selected")
	}
}

func visibleBranchRows(tw *widget.TreeViewWidget) []*treeview.TreeViewItem {
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

func TestShiftClickRangeKeepsTheClickedItemCurrentAfterARerender(t *testing.T) {
	v, tw := bound(t)
	tw.Tree.ItemHeight = 20
	tw.SetBounds(image.Rect(0, 0, 240, 400))
	snap := fullSnapshot(t)
	v.Render(snap)

	rowY := func(item *treeview.TreeViewItem) int {
		for i, row := range visibleBranchRows(tw) {
			if row == item {
				return i*20 + 10
			}
		}
		t.Fatalf("item %q is not on screen", item.DisplayText())
		return 0
	}
	x, ok := v.Item(refs.BranchName("feature/x"))
	if !ok {
		t.Fatal("feature/x is not in the tree")
	}
	y, ok := v.Item(refs.BranchName("feature/y"))
	if !ok {
		t.Fatal("feature/y is not in the tree")
	}

	tw.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true, X: 60, Y: rowY(y)})
	tw.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false, X: 60, Y: rowY(y)})
	tw.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true, X: 60, Y: rowY(x), Mod: widget.ModShift})
	tw.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false, X: 60, Y: rowY(x), Mod: widget.ModShift})

	if tw.Tree.SelectedItem() != x {
		t.Fatalf("current before rerender = %v, want feature/x", tw.Tree.SelectedItem())
	}
	if got := len(tw.Tree.SelectedItems()); got != 2 {
		t.Fatalf("selected items before rerender = %d, want 2", got)
	}

	v.Render(snap)

	again, _ := v.Item(refs.BranchName("feature/x"))
	if tw.Tree.SelectedItem() != again {
		t.Fatalf("current after rerender = %v, want the re-rendered feature/x", tw.Tree.SelectedItem())
	}
	for _, ref := range []refs.Name{refs.BranchName("feature/x"), refs.BranchName("feature/y")} {
		item, ok := v.Item(ref)
		if !ok {
			t.Fatalf("%s missing after rerender", ref)
		}
		if !tw.Tree.IsItemSelected(item) {
			t.Fatalf("%s dropped out of the selection after rerender", ref)
		}
	}
}

func TestClearingTheStashSelectionLeavesOtherNodesSelected(t *testing.T) {
	NewView().ClearStashSelection()
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	selections := 0
	v.OnSelect = func(refs.Name) { selections++ }
	main, _ := v.Item(refs.BranchName("main"))
	stash, _ := v.Item(StashRef(1))

	v.ClearStashSelection()
	tw.Tree.SetSelectedItem(main)
	v.ClearStashSelection()
	if tw.Tree.SelectedItem() != main {
		t.Fatal("clearing the stash selection dropped a selected branch")
	}

	tw.Tree.SetSelectedItem(stash)
	v.ClearStashSelection()
	if tw.Tree.SelectedItem() != nil {
		t.Fatal("the stash stayed selected")
	}
	if selections != 2 || tw.Tree.OnSelectedItemChanged == nil {
		t.Fatalf("OnSelect ran %d times, want only the user's two selections", selections)
	}
}
