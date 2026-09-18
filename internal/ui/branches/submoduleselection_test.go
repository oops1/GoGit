package branches

import "testing"

func TestTheSelectedSubmoduleSurvivesARefresh(t *testing.T) {
	if _, ok := NewView().SelectedSubmodule(); ok {
		t.Fatal("an unbound view reported a selection")
	}
	v, tw := bound(t)
	v.Render(Snapshot{})
	v.SetSubmodules(sampleSubmodules())
	if _, ok := v.SelectedSubmodule(); ok {
		t.Fatal("a selection was reported before the user chose one")
	}
	item, _ := v.SubmoduleItem("tool")
	tw.Tree.SetSelectedItem(item)

	v.SetSubmodules(sampleSubmodules())

	if sub, ok := v.SelectedSubmodule(); !ok || sub.Path != "tool" {
		t.Fatalf("SelectedSubmodule = %+v, %v", sub, ok)
	}
}
