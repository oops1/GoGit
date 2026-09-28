package branches

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/repo"
)

func TestRemotePairsMatchesByNameWhenNoUpstream(t *testing.T) {
	local := []Branch{{Name: refs.BranchName("develop")}}
	remotes := []Remote{{Name: "origin", Branches: []Branch{{Name: refs.RemoteBranchName("origin", "develop")}}}}

	paired, consumed := remotePairs(local, remotes)

	if got := paired[refs.BranchName("develop")]; got != "origin" {
		t.Fatalf("paired[develop] = %q, want origin", got)
	}
	if !consumed[refs.RemoteBranchName("origin", "develop")] {
		t.Fatal("origin/develop must be marked consumed")
	}
}

func TestRemotePairsPrefersUpstreamOverNameMatch(t *testing.T) {
	local := []Branch{{Name: refs.BranchName("feature/a"), Upstream: refs.RemoteBranchName("origin", "feature/b")}}
	remotes := []Remote{{Name: "origin", Branches: []Branch{
		{Name: refs.RemoteBranchName("origin", "feature/a")},
		{Name: refs.RemoteBranchName("origin", "feature/b")},
	}}}

	paired, consumed := remotePairs(local, remotes)

	if got := paired[refs.BranchName("feature/a")]; got != "origin" {
		t.Fatalf("paired[feature/a] = %q, want origin", got)
	}
	if !consumed[refs.RemoteBranchName("origin", "feature/b")] {
		t.Fatal("the upstream ref must be marked consumed")
	}
	if consumed[refs.RemoteBranchName("origin", "feature/a")] {
		t.Fatal("origin/feature/a has no local pair and must stay unconsumed")
	}
}

func TestRemotePairsLeavesUnmatchedBranchesAlone(t *testing.T) {
	local := []Branch{{Name: refs.BranchName("scratch")}}
	remotes := []Remote{{Name: "origin", Branches: []Branch{{Name: refs.RemoteBranchName("origin", "main")}}}}

	paired, consumed := remotePairs(local, remotes)

	if _, ok := paired[refs.BranchName("scratch")]; ok {
		t.Fatal("a local branch without a matching remote must stay unpaired")
	}
	if len(consumed) != 0 {
		t.Fatalf("consumed = %v, want none", consumed)
	}
}

func TestRemotePairsIgnoresTheSymbolicRemoteHead(t *testing.T) {
	local := []Branch{{Name: refs.BranchName("main")}}
	remotes := []Remote{{
		Name: "origin",
		Head: refs.RemoteBranchName("origin", "main"),
		Branches: []Branch{
			{Name: refs.Name("refs/remotes/origin/HEAD"), SymbolicTarget: refs.RemoteBranchName("origin", "main")},
			{Name: refs.RemoteBranchName("origin", "main")},
		},
	}}

	paired, _ := remotePairs(local, remotes)

	if got := paired[refs.BranchName("main")]; got != "origin" {
		t.Fatalf("paired[main] = %q, want origin", got)
	}
}

func TestViewMergesLocalAndRemoteBranchesWithMatchingNames(t *testing.T) {
	v, tw := bound(t)
	snap := Snapshot{
		Local: []Branch{
			{Name: refs.BranchName("develop"), Target: oid(t, "11")},
			{Name: refs.BranchName("scratch"), Target: oid(t, "22")},
		},
		Remotes: []Remote{{
			Name:     "origin",
			Branches: []Branch{{Name: refs.RemoteBranchName("origin", "develop"), Target: oid(t, "11")}},
		}},
	}
	v.Render(snap)

	want := []string{"develop = origin", "scratch"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local outline = %v, want %v", got, want)
	}
	item, ok := v.Item(refs.BranchName("develop"))
	if !ok {
		t.Fatal("the merged row must still be tracked by the local ref")
	}
	if item.DisplayText() != "develop = origin" {
		t.Fatalf("merged row text = %q", item.DisplayText())
	}
}

func TestFlowSectionHeaderCountsMergedPairsOnce(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	snap := Snapshot{
		Current: "feature/x",
		Local: []Branch{
			{Name: refs.BranchName("feature/x"), Target: oid(t, "11")},
			{Name: refs.BranchName("feature/y"), Target: oid(t, "22")},
		},
		Remotes: []Remote{{
			Name: "origin",
			Branches: []Branch{
				{Name: refs.RemoteBranchName("origin", "feature/x"), Target: oid(t, "11")},
				{Name: refs.RemoteBranchName("origin", "feature/y"), Target: oid(t, "22")},
				{Name: refs.RemoteBranchName("origin", "feature/z"), Target: oid(t, "33")},
			},
		}},
	}
	v.SetOptions(Options{FlowSections: true})
	v.Render(snap)

	want := []string{"Features (3)", "  origin/z", "  x = origin", "  y = origin"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindFeature)); !slices.Equal(got, want) {
		t.Fatalf("features section = %v, want %v (two pairs merged, one remote left alone)", got, want)
	}
}

func TestFlowSectionsMergeStillAppliesWhenFlowSectionsOptionIsOff(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	v.Render(flowFixture(t))

	want := []string{"Features (2)", "  x = origin", "  y"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindFeature)); !slices.Equal(got, want) {
		t.Fatalf("features section = %v, want %v", got, want)
	}
}

func TestViewShowsDivergenceOnAMergedLocalRemoteRow(t *testing.T) {
	v, tw := bound(t)
	snap := Snapshot{
		Local:   []Branch{{Name: refs.BranchName("develop"), Target: oid(t, "11")}},
		Remotes: []Remote{{Name: "origin", Branches: []Branch{{Name: refs.RemoteBranchName("origin", "develop"), Target: oid(t, "22")}}}},
	}
	v.Render(snap)
	v.SetDivergence(map[refs.Name]repo.Divergence{refs.BranchName("develop"): {Ahead: 3, Behind: 1}})

	local, _, _ := roots(t, tw)
	findChild(t, local, "develop = origin ↑3 ↓1")
}

func TestViewLeavesAnUnmatchedRemoteBranchAsItsOwnRowInFlowSection(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	snap := Snapshot{
		Local: []Branch{{Name: refs.BranchName("feature/x"), Target: oid(t, "11")}},
		Remotes: []Remote{{
			Name: "origin",
			Branches: []Branch{
				{Name: refs.RemoteBranchName("origin", "feature/x"), Target: oid(t, "11")},
				{Name: refs.RemoteBranchName("origin", "feature/z"), Target: oid(t, "33")},
			},
		}},
	}
	v.SetOptions(Options{FlowSections: true})
	v.Render(snap)

	want := []string{"Features (2)", "  origin/z", "  x = origin"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindFeature)); !slices.Equal(got, want) {
		t.Fatalf("features section = %v, want %v", got, want)
	}
}
