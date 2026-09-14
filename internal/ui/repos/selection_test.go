package repos

import "testing"

func TestRenderKeepsTheSelectedRepositoryWithoutReportingANewSelection(t *testing.T) {
	reg, _ := newTestRegistry(t)
	v, tw := bound(t)
	v.Render(reg, nil)
	selections := 0
	v.OnSelect = func(string) { selections++ }
	var id string
	for candidate, item := range v.itemByID {
		if item.DisplayText() == "Main" {
			id = candidate
		}
	}
	item, ok := v.Item(id)
	if !ok {
		t.Fatal("Main is not in the tree")
	}
	tw.Tree.SetSelectedItem(item)

	v.Render(reg, nil)

	again, _ := v.Item(id)
	if tw.Tree.SelectedItem() != again || again == item {
		t.Fatalf("selected = %v, want the re-rendered Main", tw.Tree.SelectedItem())
	}
	if selections != 1 {
		t.Fatalf("OnSelect ran %d times, want only the user's selection", selections)
	}
	if tw.Tree.OnSelectedItemChanged == nil {
		t.Fatal("the selection handler was not put back")
	}
}
