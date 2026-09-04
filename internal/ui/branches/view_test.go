package branches

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
)

func TestMain(m *testing.M) {
	cat, err := i18n.Builtin()
	if err != nil {
		panic(err)
	}
	cat.Register()
	m.Run()
}

func bound(t *testing.T) (*View, *widget.TreeViewWidget) {
	t.Helper()
	v := NewView()
	tw := widget.NewTreeViewWidget()
	v.Bind(tw)
	return v, tw
}

func fullSnapshot(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{
		Current: "feature/x",
		Local: []Branch{
			{Name: refs.BranchName("feature/x"), Target: oid(t, "11")},
			{Name: refs.BranchName("feature/y"), Target: oid(t, "22")},
			{Name: refs.BranchName("main"), Target: oid(t, "33")},
		},
		Remotes: []Remote{
			{
				Name: "origin",
				Head: refs.RemoteBranchName("origin", "main"),
				Branches: []Branch{
					{
						Name:           refs.Name("refs/remotes/origin/HEAD"),
						Target:         oid(t, "33"),
						SymbolicTarget: refs.RemoteBranchName("origin", "main"),
					},
					{Name: refs.RemoteBranchName("origin", "feature/x"), Target: oid(t, "11")},
					{Name: refs.RemoteBranchName("origin", "main"), Target: oid(t, "33")},
				},
			},
		},
		Tags: []Tag{
			{Name: refs.TagName("release/1.0"), Target: oid(t, "44"), Peeled: oid(t, "55")},
			{Name: refs.TagName("v1.0"), Target: oid(t, "66")},
		},
		HasStash: true,
	}
}

func findChild(t *testing.T, item *treeview.TreeViewItem, text string) *treeview.TreeViewItem {
	t.Helper()
	for _, child := range item.Children {
		if child.DisplayText() == text {
			return child
		}
	}
	t.Fatalf("child %q not found among %v", text, childTexts(item))
	return nil
}

func childTexts(item *treeview.TreeViewItem) []string {
	out := make([]string, 0, len(item.Children))
	for _, child := range item.Children {
		out = append(out, child.DisplayText())
	}
	return out
}

func roots(t *testing.T, tw *widget.TreeViewWidget) (local, remotes, tags *treeview.TreeViewItem) {
	t.Helper()
	items := tw.Tree.Roots()
	if len(items) < 3 {
		t.Fatalf("roots = %d, want at least 3", len(items))
	}
	return items[0], items[1], items[2]
}

func TestRenderBuildsLocalGroupWithNestedBranches(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))

	local, _, _ := roots(t, tw)
	if local.DisplayText() != "Local" {
		t.Fatalf("local root text = %q", local.DisplayText())
	}
	if len(local.Children) != 2 {
		t.Fatalf("local children = %v, want feature and main", childTexts(local))
	}
	feature := findChild(t, local, "feature")
	if len(feature.Children) != 2 {
		t.Fatalf("feature children = %v", childTexts(feature))
	}
	if feature.Children[0].DisplayText() != "x" {
		t.Fatalf("feature/x text = %q", feature.Children[0].DisplayText())
	}
	if feature.Children[0].Icon == feature.Children[1].Icon {
		t.Fatal("the current branch must use a different icon than a regular branch")
	}
	if feature.Children[1].DisplayText() != "y" {
		t.Fatalf("feature/y text = %q", feature.Children[1].DisplayText())
	}
	main := findChild(t, local, "main")
	if main.DisplayText() != "main" {
		t.Fatalf("main text = %q", main.DisplayText())
	}
}

func TestRenderBuildsRemotesGroupWithHeadArrowAndNesting(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))

	_, remotesRoot, _ := roots(t, tw)
	if remotesRoot.DisplayText() != "Remotes" {
		t.Fatalf("remotes root text = %q", remotesRoot.DisplayText())
	}
	if len(remotesRoot.Children) != 1 {
		t.Fatalf("remotes children = %v, want one remote", childTexts(remotesRoot))
	}
	origin := remotesRoot.Children[0]
	if origin.DisplayText() != "origin" {
		t.Fatalf("remote node text = %q", origin.DisplayText())
	}
	if len(origin.Children) != 2 {
		t.Fatalf("origin children = %v, the symbolic HEAD must not get a row of its own", childTexts(origin))
	}
	feature := findChild(t, origin, "feature")
	if len(feature.Children) != 1 || feature.Children[0].DisplayText() != "x" {
		t.Fatalf("origin/feature children = %v", childTexts(feature))
	}
	main := findChild(t, origin, "main")
	if main.DisplayText() != "main" {
		t.Fatalf("origin/main text = %q", main.DisplayText())
	}
	if main.Icon == feature.Children[0].Icon {
		t.Fatal("the branch the remote HEAD points at must carry its own icon")
	}
}

func TestRenderBuildsTagsGroupWithNesting(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))

	_, _, tagsRoot := roots(t, tw)
	if tagsRoot.DisplayText() != "Tags" {
		t.Fatalf("tags root text = %q", tagsRoot.DisplayText())
	}
	if len(tagsRoot.Children) != 2 {
		t.Fatalf("tags children = %v", childTexts(tagsRoot))
	}
	release := findChild(t, tagsRoot, "release")
	if len(release.Children) != 1 || release.Children[0].DisplayText() != "1.0" {
		t.Fatalf("tags/release children = %v", childTexts(release))
	}
	v1 := findChild(t, tagsRoot, "v1.0")
	if v1.DisplayText() != "v1.0" {
		t.Fatalf("tags/v1.0 text = %q", v1.DisplayText())
	}
}

func TestRenderAddsStashRootOnlyWhenPresent(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	items := tw.Tree.Roots()
	if len(items) != 4 {
		t.Fatalf("roots = %d, want 4 including stash", len(items))
	}
	if items[3].DisplayText() != "Stash" {
		t.Fatalf("stash root text = %q", items[3].DisplayText())
	}

	snap := fullSnapshot(t)
	snap.HasStash = false
	v2, tw2 := bound(t)
	v2.Render(snap)
	if len(tw2.Tree.Roots()) != 3 {
		t.Fatalf("roots = %d, want 3 without stash", len(tw2.Tree.Roots()))
	}
}

func TestRenderMarksDetachedHeadUnderLocal(t *testing.T) {
	v, tw := bound(t)
	snap := Snapshot{
		Detached: true,
		Current:  oid(t, "aa").String(),
		HeadID:   oid(t, "aa"),
		Local: []Branch{
			{Name: refs.BranchName("main"), Target: oid(t, "bb")},
		},
	}
	v.Render(snap)

	local, _, _ := roots(t, tw)
	if len(local.Children) != 2 {
		t.Fatalf("local children = %v, want detached marker and main", childTexts(local))
	}
	want := "Detached HEAD " + shortID(oid(t, "aa"))
	if local.Children[0].DisplayText() != want {
		t.Fatalf("detached marker = %q, want %q", local.Children[0].DisplayText(), want)
	}
	if local.Children[0].Icon == nil {
		t.Fatal("detached HEAD must have an icon")
	}
	if local.Children[1].DisplayText() != "main" {
		t.Fatalf("main text = %q, want plain (no current marker)", local.Children[1].DisplayText())
	}
}

func TestRenderOnEmptySnapshotProducesEmptyGroups(t *testing.T) {
	v, tw := bound(t)
	v.Render(Snapshot{})
	items := tw.Tree.Roots()
	if len(items) != 3 {
		t.Fatalf("roots = %d, want 3", len(items))
	}
	for _, root := range items {
		if len(root.Children) != 0 {
			t.Fatalf("root %q has children %v, want none", root.DisplayText(), childTexts(root))
		}
	}
}

func TestRenderDefaultsLocalRemotesExpandedTagsCollapsed(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))

	local, remotesRoot, tagsRoot := roots(t, tw)
	if !local.Expanded {
		t.Fatal("local root must default to expanded")
	}
	if !remotesRoot.Expanded {
		t.Fatal("remotes root must default to expanded")
	}
	if !remotesRoot.Children[0].Expanded {
		t.Fatal("remote node must default to expanded")
	}
	if tagsRoot.Expanded {
		t.Fatal("tags root must default to collapsed")
	}
}

func TestRenderPreservesCollapsedStateAcrossRenders(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	local, _, _ := roots(t, tw)
	tw.Tree.CollapseItem(local)

	v.Render(fullSnapshot(t))
	local, _, _ = roots(t, tw)
	if local.Expanded {
		t.Fatal("collapsed local root must stay collapsed across renders")
	}
}

func TestRenderPreservesExpandedTagsAcrossRenders(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	_, _, tagsRoot := roots(t, tw)
	tw.Tree.ExpandItem(tagsRoot)

	v.Render(fullSnapshot(t))
	_, _, tagsRoot2 := roots(t, tw)
	if !tagsRoot2.Expanded {
		t.Fatal("expanded tags root must stay expanded across renders")
	}
}

func TestRenderPreservesCollapsedFolderAcrossRenders(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	local, _, _ := roots(t, tw)
	feature := findChild(t, local, "feature")
	tw.Tree.CollapseItem(feature)

	v.Render(fullSnapshot(t))
	local, _, _ = roots(t, tw)
	feature = findChild(t, local, "feature")
	if feature.Expanded {
		t.Fatal("collapsed folder must stay collapsed across renders")
	}
}

func TestRenderKeepsNewRemoteExpandedAfterUnrelatedCollapse(t *testing.T) {
	v, tw := bound(t)
	snap := fullSnapshot(t)
	v.Render(snap)
	local, _, _ := roots(t, tw)
	tw.Tree.CollapseItem(local)

	snap.Remotes = append(snap.Remotes, Remote{
		Name:     "upstream",
		Branches: []Branch{{Name: refs.RemoteBranchName("upstream", "dev"), Target: oid(t, "77")}},
	})
	v.Render(snap)

	local, remotesRoot, _ := roots(t, tw)
	if local.Expanded {
		t.Fatal("local root must still be collapsed")
	}
	upstream := findChild(t, remotesRoot, "upstream")
	if !upstream.Expanded {
		t.Fatal("new remote node must default to expanded")
	}
}

func TestOnActivateFiresWithRefNameForLeafItems(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	item, ok := v.Item(refs.BranchName("main"))
	if !ok {
		t.Fatal("item for main branch not tracked")
	}

	var got refs.Name
	v.OnActivate = func(ref refs.Name) { got = ref }
	tw.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
	if got != refs.BranchName("main") {
		t.Fatalf("OnActivate ref = %q, want refs/heads/main", got)
	}
}

func TestOnActivateIgnoresFolderNodes(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	local, _, _ := roots(t, tw)
	feature := findChild(t, local, "feature")

	called := false
	v.OnActivate = func(refs.Name) { called = true }
	tw.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: feature})
	if called {
		t.Fatal("folder node must not trigger OnActivate")
	}
}

func TestOnActivateNotSetDoesNotPanic(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	item, _ := v.Item(refs.BranchName("main"))
	tw.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
}

func TestOnSelectFiresWithRefNameOnSelectedItemChanged(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	item, ok := v.Item(refs.TagName("v1.0"))
	if !ok {
		t.Fatal("item for v1.0 tag not tracked")
	}

	var got refs.Name
	v.OnSelect = func(ref refs.Name) { got = ref }
	tw.Tree.SetSelectedItem(item)
	if got != refs.TagName("v1.0") {
		t.Fatalf("OnSelect ref = %q, want refs/tags/v1.0", got)
	}
}

func TestOnSelectIgnoresNilSelection(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	item, _ := v.Item(refs.TagName("v1.0"))
	tw.Tree.SetSelectedItem(item)

	called := false
	v.OnSelect = func(refs.Name) { called = true }
	tw.Tree.SetSelectedItem(nil)
	if called {
		t.Fatal("nil selection must not trigger OnSelect")
	}
}

func TestOnSelectIgnoresUnknownItemAndMissingHandler(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	tw.Tree.SetSelectedItem(treeview.NewItem("ghost"))

	called := false
	v.OnSelect = func(refs.Name) { called = true }
	tw.Tree.SetSelectedItem(treeview.NewItem("ghost2"))
	if called {
		t.Fatal("unknown item must not trigger OnSelect")
	}

	v.OnSelect = nil
	item, _ := v.Item(refs.TagName("v1.0"))
	tw.Tree.SetSelectedItem(item)
}

func TestItemReturnsFalseForUnknownRef(t *testing.T) {
	v, _ := bound(t)
	v.Render(fullSnapshot(t))
	if _, ok := v.Item(refs.BranchName("ghost")); ok {
		t.Fatal("expected not found")
	}
}

func TestCaptureExpandedIsNoOpBeforeBind(t *testing.T) {
	v := NewView()
	v.captureExpanded()
}

func TestRenderAssignsAnIconToEveryNode(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))

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

func TestRenderUsesTheFolderIconForGroupNodes(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	local, remotesRoot, tagsRoot := roots(t, tw)
	folder := local.Icon
	if folder == nil {
		t.Fatal("local group must have an icon")
	}
	if remotesRoot.Icon != folder || tagsRoot.Icon != folder {
		t.Fatal("every group node must share the folder icon")
	}
	origin := remotesRoot.Children[0]
	if origin.Icon != folder {
		t.Fatal("a remote node is a group and must use the folder icon")
	}
}

func TestRenderUsesDistinctIconsForRemoteBranchesAndTags(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	local, remotesRoot, tagsRoot := roots(t, tw)

	main := findChild(t, local, "main")
	origin := remotesRoot.Children[0]
	originMain := findChild(t, origin, "main")
	v1 := findChild(t, tagsRoot, "v1.0")

	if main.Icon == nil || originMain.Icon == nil || v1.Icon == nil {
		t.Fatal("every leaf must have an icon")
	}
	if main.Icon == originMain.Icon {
		t.Fatal("a local branch and a remote branch must use different icons")
	}
	if main.Icon == v1.Icon || originMain.Icon == v1.Icon {
		t.Fatal("a tag must use a different icon than a branch")
	}
}

func TestRenderUsesTheStashIcon(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	items := tw.Tree.Roots()
	stash := items[len(items)-1]
	if stash.Icon == nil {
		t.Fatal("stash node must have an icon")
	}
	local, _, _ := roots(t, tw)
	main := findChild(t, local, "main")
	if stash.Icon == main.Icon {
		t.Fatal("stash must use a different icon than a branch")
	}
}

func TestStashItemIsTrackedForSelection(t *testing.T) {
	v, tw := bound(t)
	v.Render(fullSnapshot(t))
	item, ok := v.Item(stashRefName)
	if !ok {
		t.Fatal("stash item not tracked")
	}

	var got refs.Name
	v.OnActivate = func(ref refs.Name) { got = ref }
	tw.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})
	if got != stashRefName {
		t.Fatalf("OnActivate ref = %q, want refs/stash", got)
	}
}
