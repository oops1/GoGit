package branches

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func obsoleteFixture(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{
		Current: "main",
		Local: []Branch{
			{
				Name:     refs.BranchName("main"),
				Target:   oid(t, "11"),
				Upstream: refs.RemoteBranchName("origin", "main"),
			},
			{
				Name:     refs.BranchName("kept"),
				Target:   oid(t, "22"),
				Upstream: refs.RemoteBranchName("origin", "kept"),
			},
			{
				Name:     refs.BranchName("gone"),
				Target:   oid(t, "33"),
				Upstream: refs.RemoteBranchName("origin", "gone"),
			},
			{Name: refs.BranchName("local-only"), Target: oid(t, "44")},
		},
		Remotes: []Remote{{
			Name: "origin",
			Branches: []Branch{
				{Name: refs.RemoteBranchName("origin", "main"), Target: oid(t, "11")},
				{Name: refs.RemoteBranchName("origin", "kept"), Target: oid(t, "22")},
			},
		}},
	}
}

func TestObsoleteLocalFindsBranchesWhoseRemoteBranchIsGone(t *testing.T) {
	got := ObsoleteLocal(obsoleteFixture(t))
	want := []refs.Name{refs.BranchName("gone")}
	if !slices.Equal(got, want) {
		t.Fatalf("obsolete = %v, want %v", got, want)
	}
}

func TestObsoleteLocalLeavesTheCurrentBranchAlone(t *testing.T) {
	snap := obsoleteFixture(t)
	snap.Current = "gone"
	if got := ObsoleteLocal(snap); len(got) != 0 {
		t.Fatalf("obsolete = %v, want none while it is checked out", got)
	}
}

func TestObsoleteLocalCountsTheCurrentBranchWhenHeadIsDetached(t *testing.T) {
	snap := obsoleteFixture(t)
	snap.Current, snap.Detached = "gone", true
	want := []refs.Name{refs.BranchName("gone")}
	if got := ObsoleteLocal(snap); !slices.Equal(got, want) {
		t.Fatalf("obsolete = %v, want %v", got, want)
	}
}

func TestSelectObsoleteSelectsTheOnlyObsoleteBranch(t *testing.T) {
	v, tw := bound(t)
	v.Render(obsoleteFixture(t))
	var picked refs.Name
	v.OnSelect = func(ref refs.Name) { picked = ref }

	stale := v.SelectObsolete()
	if len(stale) != 1 || stale[0] != refs.BranchName("gone") {
		t.Fatalf("obsolete = %v", stale)
	}
	if picked != refs.BranchName("gone") {
		t.Fatalf("selection = %q, want refs/heads/gone", picked)
	}
	if item, _ := v.Item(refs.BranchName("gone")); tw.Tree.SelectedItem() != item {
		t.Fatal("the obsolete branch must end up selected in the tree")
	}
}

func manyObsoleteFixture(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{
		Current: "main",
		Local: []Branch{
			{
				Name:     refs.BranchName("main"),
				Target:   oid(t, "11"),
				Upstream: refs.RemoteBranchName("origin", "main"),
			},
			{
				Name:     refs.BranchName("gone-a"),
				Target:   oid(t, "22"),
				Upstream: refs.RemoteBranchName("origin", "gone-a"),
			},
			{
				Name:     refs.BranchName("gone-b"),
				Target:   oid(t, "33"),
				Upstream: refs.RemoteBranchName("origin", "gone-b"),
			},
		},
		Remotes: []Remote{{
			Name:     "origin",
			Branches: []Branch{{Name: refs.RemoteBranchName("origin", "main"), Target: oid(t, "11")}},
		}},
	}
}

func TestSelectObsoleteSelectsEveryObsoleteBranchAtOnce(t *testing.T) {
	v, tw := bound(t)
	v.Render(manyObsoleteFixture(t))
	var picked refs.Name
	v.OnSelect = func(ref refs.Name) { picked = ref }

	want := []refs.Name{refs.BranchName("gone-a"), refs.BranchName("gone-b")}
	stale := v.SelectObsolete()
	if !slices.Equal(stale, want) {
		t.Fatalf("obsolete = %v, want %v", stale, want)
	}

	if got := len(tw.Tree.SelectedItems()); got != len(want) {
		t.Fatalf("selected items = %d, want %d", got, len(want))
	}
	for _, ref := range want {
		item, ok := v.Item(ref)
		if !ok {
			t.Fatalf("%s not tracked in the tree", ref)
		}
		if !tw.Tree.IsItemSelected(item) {
			t.Fatalf("%s must end up selected in the tree", ref)
		}
	}
	if picked != refs.BranchName("gone-b") {
		t.Fatalf("current selection = %q, want the last obsolete branch", picked)
	}
}

func TestSelectObsoleteMultiSelectionSurvivesARerender(t *testing.T) {
	v, tw := bound(t)
	snap := manyObsoleteFixture(t)
	v.Render(snap)
	v.SelectObsolete()

	v.Render(snap)

	if got := len(tw.Tree.SelectedItems()); got != 2 {
		t.Fatalf("selected items after rerender = %d, want 2", got)
	}
	for _, ref := range []refs.Name{refs.BranchName("gone-a"), refs.BranchName("gone-b")} {
		item, ok := v.Item(ref)
		if !ok {
			t.Fatalf("%s not tracked after rerender", ref)
		}
		if !tw.Tree.IsItemSelected(item) {
			t.Fatalf("%s dropped out of the selection after rerender", ref)
		}
	}
	if tw.Tree.SelectedItem() == nil {
		t.Fatal("the rerender must keep a current item for the multi-selection")
	}
}

func TestSelectObsoleteOnAnEmptyTreeChangesNothing(t *testing.T) {
	v, tw := bound(t)
	v.Render(Snapshot{})
	if stale := v.SelectObsolete(); len(stale) != 0 {
		t.Fatalf("obsolete = %v, want none", stale)
	}
	if tw.Tree.SelectedItem() != nil {
		t.Fatal("nothing must be selected")
	}
}

func TestSelectObsoleteBeforeBindReportsWithoutSelecting(t *testing.T) {
	v := NewView()
	v.last = obsoleteFixture(t)
	if stale := v.SelectObsolete(); len(stale) != 1 {
		t.Fatalf("obsolete = %v, want one", stale)
	}
}

func TestSelectObsoleteReachesABranchShownInAFlowSection(t *testing.T) {
	v, tw := bound(t)
	snap := obsoleteFixture(t)
	snap.Local = []Branch{{
		Name:     refs.BranchName("feature/lost"),
		Target:   oid(t, "55"),
		Upstream: refs.RemoteBranchName("origin", "feature/lost"),
	}}
	v.SetFlow(ops.DefaultFlowConfig(), true)
	v.Render(snap)

	stale := v.SelectObsolete()
	if len(stale) != 1 || stale[0] != refs.BranchName("feature/lost") {
		t.Fatalf("obsolete = %v", stale)
	}
	item, ok := v.Item(refs.BranchName("feature/lost"))
	if !ok || tw.Tree.SelectedItem() != item {
		t.Fatal("the obsolete feature branch must end up selected")
	}
}
