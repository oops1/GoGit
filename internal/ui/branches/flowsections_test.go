package branches

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func flowFixture(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{
		Current: "feature/x",
		Local: []Branch{
			{Name: refs.BranchName("develop"), Target: oid(t, "11")},
			{Name: refs.BranchName("feature/x"), Target: oid(t, "22")},
			{Name: refs.BranchName("feature/y"), Target: oid(t, "33")},
			{Name: refs.BranchName("release/1.0"), Target: oid(t, "44")},
		},
		Remotes: []Remote{{
			Name: "origin",
			Head: refs.RemoteBranchName("origin", "develop"),
			Branches: []Branch{
				{
					Name:           refs.Name("refs/remotes/origin/HEAD"),
					Target:         oid(t, "11"),
					SymbolicTarget: refs.RemoteBranchName("origin", "develop"),
				},
				{Name: refs.RemoteBranchName("origin", "feature/x"), Target: oid(t, "22")},
				{Name: refs.RemoteBranchName("origin", "develop"), Target: oid(t, "11")},
			},
		}},
	}
}

func flowKey(kind string) string { return flowGroupKey + "/" + kind }

func TestFlowSectionsStandAboveLocalBranchesAndTakeTheirBranches(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	v.Render(flowFixture(t))

	if got := v.keyByItem[tw.Tree.Roots()[0]]; got != flowKey(ops.FlowKindFeature) {
		t.Fatalf("first root key = %q, want the feature section", got)
	}
	want := []string{"Features (2)", "  x", "  y"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindFeature)); !slices.Equal(got, want) {
		t.Fatalf("features section = %v, want %v", got, want)
	}
	want = []string{"Releases (1)", "  1.0"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindRelease)); !slices.Equal(got, want) {
		t.Fatalf("releases section = %v, want %v", got, want)
	}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, []string{"develop"}) {
		t.Fatalf("local branches = %v, want only develop", got)
	}
}

func TestFlowSectionsKeepRemoteBranchesOutUnlessAskedFor(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	v.Render(flowFixture(t))
	want := []string{"Remotes", "  origin", "    develop", "    feature", "      x"}
	if got := sectionOutline(t, v, tw.Tree, remotesGroupKey); !slices.Equal(got, want) {
		t.Fatalf("remotes = %v, want %v", got, want)
	}

	v.SetOptions(Options{FlowSections: true, Grouping: Grouping{ByPath: true}})
	want = []string{"Features (3)", "  origin", "    x", "  x", "  y"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindFeature)); !slices.Equal(got, want) {
		t.Fatalf("features section = %v, want %v", got, want)
	}
	want = []string{"Remotes", "  origin", "    develop"}
	if got := sectionOutline(t, v, tw.Tree, remotesGroupKey); !slices.Equal(got, want) {
		t.Fatalf("remotes = %v, want %v", got, want)
	}
}

func TestFlowSectionsAreAbsentWithoutAConfiguredFlow(t *testing.T) {
	v, tw := bound(t)
	v.Render(flowFixture(t))
	if cfg, configured := v.Flow(); configured || cfg.Develop != "" {
		t.Fatalf("flow = %+v, configured = %v", cfg, configured)
	}
	want := []string{"develop", "feature", "  x", "  y", "release", "  1.0"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local branches = %v, want %v", got, want)
	}
}

func TestLightFlowOnlyKeepsTheFeatureSection(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultLightFlowConfig(), true)
	v.Render(flowFixture(t))
	want := []string{"Features (2)", "  x", "  y"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindFeature)); !slices.Equal(got, want) {
		t.Fatalf("features section = %v, want %v", got, want)
	}
	want = []string{"develop", "release", "  1.0"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local branches = %v, want %v", got, want)
	}
}

func TestTheCurrentFlowBranchKeepsItsOwnIcon(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	v.Render(flowFixture(t))
	features := rootByKey(t, v, tw.Tree, flowKey(ops.FlowKindFeature))
	if features.Children[0].Icon == features.Children[1].Icon {
		t.Fatal("the current feature must use a different icon than a plain one")
	}
}

func TestFlowBranchNameKeepsAnUnprefixedName(t *testing.T) {
	v := NewView()
	v.SetFlow(ops.DefaultFlowConfig(), true)
	if got := v.flowBranchName("develop"); got != "develop" {
		t.Fatalf("flowBranchName(develop) = %q", got)
	}
}

func TestLightFlowPutsTheBaseBranchAboveTheSections(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultLightFlowConfig(), true)
	s := flowFixture(t)
	s.Local = append(s.Local, Branch{Name: refs.BranchName("master"), Target: oid(t, "55")})
	v.Render(s)

	root := tw.Tree.Roots()[0]
	if root.Text != "master" {
		t.Fatalf("first root = %q, want the base branch", root.Text)
	}
	if ref, ok := v.idByItem[root]; !ok || ref != refs.BranchName("master") {
		t.Fatalf("first root tracks %v, %v", ref, ok)
	}
	want := []string{"develop", "release", "  1.0"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local branches = %v, want the base branch taken out", got)
	}
}

func TestFullFlowLeavesTheBaseBranchInLocal(t *testing.T) {
	v, tw := bound(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	s := flowFixture(t)
	s.Local = append(s.Local, Branch{Name: refs.BranchName("master"), Target: oid(t, "55")})
	v.Render(s)

	if got := v.keyByItem[tw.Tree.Roots()[0]]; got != flowKey(ops.FlowKindFeature) {
		t.Fatalf("first root key = %q, want the feature section", got)
	}
	if got := localOutline(t, v, tw.Tree); !slices.Contains(got, "master") {
		t.Fatalf("local branches = %v, want master among them", got)
	}
}
