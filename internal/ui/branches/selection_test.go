package branches

import (
	"testing"

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
