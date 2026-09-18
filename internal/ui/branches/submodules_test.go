package branches

import (
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
)

func sampleSubmodules() []ops.Submodule {
	return []ops.Submodule{
		{Name: "lib", Path: "libs/lib", State: ops.SubmoduleStateUpToDate},
		{Name: "docs", Path: "docs", State: ops.SubmoduleStateNotInitialized},
		{Name: "tool", Path: "tool", State: ops.SubmoduleStateModified},
		{Name: "api", Path: "api", State: ops.SubmoduleStateNewCommits},
		{Name: "merge", Path: "merge", State: ops.SubmoduleStateConflict},
	}
}

func lastRoot(tw *widget.TreeViewWidget) *treeview.TreeViewItem {
	roots := tw.Tree.Roots()
	return roots[len(roots)-1]
}

func TestSubmodulesGroupAppearsOnlyWithSubmodules(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	rootsBefore := len(tw.Tree.Roots())

	v.SetSubmodules(sampleSubmodules())

	if len(tw.Tree.Roots()) != rootsBefore+1 {
		t.Fatalf("roots = %d, want one more than %d", len(tw.Tree.Roots()), rootsBefore)
	}
	group := lastRoot(tw)
	if group.DisplayText() != i18n.T("Pane.Branches.Submodules") || group.Icon == nil {
		t.Fatalf("group = %q", group.DisplayText())
	}
	want := []string{
		"libs/lib (" + i18n.T("Pane.Branches.Submodule.UpToDate") + ")",
		"docs (" + i18n.T("Pane.Branches.Submodule.NotInitialized") + ")",
		"tool (" + i18n.T("Pane.Branches.Submodule.Modified") + ")",
		"api (" + i18n.T("Pane.Branches.Submodule.NewCommits") + ")",
		"merge (" + i18n.T("Pane.Branches.Submodule.Conflict") + ")",
	}
	if got := childTexts(group); !slices.Equal(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	for _, child := range group.Children {
		if child.Icon == nil {
			t.Fatalf("%q has no icon", child.DisplayText())
		}
	}
	if !slices.Equal(v.Submodules(), sampleSubmodules()) {
		t.Fatalf("Submodules = %+v", v.Submodules())
	}

	v.SetSubmodules(nil)
	if len(tw.Tree.Roots()) != rootsBefore {
		t.Fatal("the group stayed after the submodules were cleared")
	}
}

func TestSetSubmodulesBeforeBindKeepsTheList(t *testing.T) {
	v := NewView()
	v.SetSubmodules(sampleSubmodules())
	if len(v.Submodules()) != 5 {
		t.Fatalf("Submodules = %+v", v.Submodules())
	}
}

func TestSubmoduleItemsOpenAndOfferTheirMenu(t *testing.T) {
	v, tw := bound(t)
	v.Render(Snapshot{})
	v.SetSubmodules(sampleSubmodules())
	item, ok := v.SubmoduleItem("tool")
	if !ok {
		t.Fatal("the tool submodule is not rendered")
	}
	if _, ok := v.SubmoduleItem("missing"); ok {
		t.Fatal("a missing submodule was found")
	}

	tw.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
	if got := tw.NodeContextMenu(item); got != nil {
		t.Fatalf("a menu without a handler = %+v", got)
	}

	var opened, asked ops.Submodule
	v.OnSubmoduleActivate = func(s ops.Submodule) { opened = s }
	v.OnSubmoduleMenu = func(s ops.Submodule) []widget.MenuItem {
		asked = s
		return []widget.MenuItem{{Text: "open"}}
	}
	refActivated := false
	v.OnActivate = func(refs.Name) { refActivated = true }

	tw.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
	if got := tw.NodeContextMenu(item); len(got) != 1 {
		t.Fatalf("menu = %+v", got)
	}
	if opened.Path != "tool" || asked.Path != "tool" || refActivated {
		t.Fatalf("opened = %+v, asked = %+v, ref activated = %v", opened, asked, refActivated)
	}
}
