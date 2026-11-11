package branches

import (
	"maps"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v4/widget"
	"github.com/oops1/headless-gui/v4/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func boundWithMainSection(t *testing.T) (*View, *widget.TreeViewWidget) {
	t.Helper()
	v := NewView()
	tw := widget.NewTreeViewWidget()
	v.Bind(tw)
	return v, tw
}

func workflowFixture(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{
		Current: "feature/x",
		Local: []Branch{
			{Name: refs.BranchName("develop"), Target: oid(t, "11"), When: at(1)},
			{Name: refs.BranchName("master"), Target: oid(t, "22"), When: at(2)},
			{Name: refs.BranchName("feature/x"), Target: oid(t, "33"), When: at(3)},
			{Name: refs.BranchName("scratch"), Target: oid(t, "44"), When: at(4)},
		},
		Remotes: []Remote{{
			Name: "origin",
			Head: refs.RemoteBranchName("origin", "develop"),
			Branches: []Branch{
				{Name: refs.RemoteBranchName("origin", "alpha"), Target: oid(t, "55"), When: at(9)},
				{Name: refs.RemoteBranchName("origin", "develop"), Target: oid(t, "11"), When: at(1)},
				{Name: refs.RemoteBranchName("origin", "feature/x"), Target: oid(t, "33"), When: at(3)},
				{Name: refs.RemoteBranchName("origin", "master"), Target: oid(t, "22"), When: at(2)},
			},
		}},
	}
}

func childByText(t *testing.T, parent *treeview.TreeViewItem, text string) *treeview.TreeViewItem {
	t.Helper()
	for _, child := range parent.Children {
		if child.DisplayText() == text {
			return child
		}
	}
	t.Fatalf("no child %q among %v", text, outline(parent.Children))
	return nil
}

func TestMainBranchNamesCollectTheWorkflowBranches(t *testing.T) {
	conventional := []string{"develop", "main", "master"}
	for _, tc := range []struct {
		name       string
		snapshot   Snapshot
		flow       ops.FlowConfig
		configured bool
		want       []string
	}{
		{name: "the usual names without git-flow", want: conventional},
		{
			name:       "the branches git-flow names",
			flow:       ops.FlowConfig{Develop: "dev", Master: "prod"},
			configured: true,
			want:       []string{"dev", "develop", "main", "master", "prod"},
		},
		{
			name:       "light git-flow has no master",
			flow:       ops.FlowConfig{Develop: "trunk"},
			configured: true,
			want:       []string{"develop", "main", "master", "trunk"},
		},
		{
			name: "git-flow that is not configured names nothing",
			flow: ops.FlowConfig{Develop: "dev", Master: "prod"},
			want: conventional,
		},
		{
			name: "the default branch of every server",
			snapshot: Snapshot{Remotes: []Remote{
				{Name: "origin", Head: refs.RemoteBranchName("origin", "stable")},
				{Name: "gitlab", Head: refs.RemoteBranchName("gitlab", "release/current")},
				{Name: "empty"},
			}},
			want: []string{"develop", "main", "master", "release/current", "stable"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Sorted(maps.Keys(MainBranchNames(tc.snapshot, tc.flow, tc.configured)))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("main branches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMainBranchesStandFirstInAGroupOfTheirOwn(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.Render(workflowFixture(t))

	first := tw.Tree.Roots()[0]
	if v.keyByItem[first] != mainGroupKey || !first.Bold {
		t.Fatalf("first root = %q (key %q, bold %v), want the main group in bold", first.Text, v.keyByItem[first], first.Bold)
	}
	want := []string{"Main", "  develop = origin", "  master = origin"}
	if got := sectionOutline(t, v, tw.Tree, mainGroupKey); !slices.Equal(got, want) {
		t.Fatalf("main group = %v, want %v", got, want)
	}
	want = []string{"feature", "  x = origin", "scratch"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local branches = %v, want the main branches taken out", got)
	}
	if ref, ok := v.idByItem[childByText(t, first, "develop = origin")]; !ok || ref != refs.BranchName("develop") {
		t.Fatalf("develop tracks %v, %v", ref, ok)
	}
}

func TestLightGitFlowKeepsItsBaseBranchInTheMainGroup(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.SetFlow(ops.FlowConfig{Develop: "develop", Remote: "origin"}, true)
	v.Render(workflowFixture(t))

	want := []string{"Main", "  develop = origin", "  master = origin"}
	if got := sectionOutline(t, v, tw.Tree, mainGroupKey); !slices.Equal(got, want) {
		t.Fatalf("main group = %v, want %v", got, want)
	}
	for _, root := range tw.Tree.Roots() {
		if v.keyByItem[root] == "" {
			t.Fatalf("a branch stands at the top level outside any group: %q", root.Text)
		}
	}
}

func TestThereIsNoMainGroupWithoutMainBranches(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.Render(Snapshot{Local: []Branch{{Name: refs.BranchName("feature/x"), Target: oid(t, "11")}}})

	for _, root := range tw.Tree.Roots() {
		if v.keyByItem[root] == mainGroupKey {
			t.Fatal("an empty main group is shown")
		}
	}
}

func TestMainBranchesFollowTheChosenSort(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.SetOptions(Options{Sort: SortByCommitTime, MainSection: true})
	v.Render(workflowFixture(t))

	want := []string{"Main", "  master = origin", "  develop = origin"}
	if got := sectionOutline(t, v, tw.Tree, mainGroupKey); !slices.Equal(got, want) {
		t.Fatalf("main group = %v, want the newest first %v", got, want)
	}
}

func TestTheCurrentMainBranchKeepsTheCurrentIcon(t *testing.T) {
	v, tw := boundWithMainSection(t)
	s := workflowFixture(t)
	s.Current = "develop"
	v.Render(s)

	main := rootByKey(t, v, tw.Tree, mainGroupKey)
	current, other := childByText(t, main, "develop = origin"), childByText(t, main, "master = origin")
	plain := childByText(t, rootByKey(t, v, tw.Tree, localGroupKey), "scratch")
	if current.Icon == other.Icon {
		t.Fatal("the current main branch must look different from the other main branch")
	}
	if other.Icon == plain.Icon {
		t.Fatal("a main branch must look different from an ordinary one")
	}
}

func TestMainBranchesComeFirstOnEveryServer(t *testing.T) {
	for _, mode := range []SortMode{SortByName, SortByNameReverseNumbers, SortByCommitTime} {
		v, tw := boundWithMainSection(t)
		v.SetOptions(Options{Sort: mode, Grouping: Grouping{ByPath: true}, MainSection: true})
		v.Render(workflowFixture(t))

		origin := childByText(t, rootByKey(t, v, tw.Tree, remotesGroupKey), "origin")
		got := outline(origin.Children)
		if len(got) < 2 || !slices.Contains(got[:2], "develop") || !slices.Contains(got[:2], "master") {
			t.Fatalf("sort %d: origin = %v, want develop and master first", mode, got)
		}
	}
}

func TestRemoteMainBranchesWearTheMainIcon(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.Render(workflowFixture(t))

	origin := childByText(t, rootByKey(t, v, tw.Tree, remotesGroupKey), "origin")
	head, master, alpha := childByText(t, origin, "develop"), childByText(t, origin, "master"), childByText(t, origin, "alpha")
	if head.Icon == master.Icon {
		t.Fatal("the default branch of the server keeps its own icon")
	}
	if master.Icon == alpha.Icon {
		t.Fatal("a main branch on the server must look different from an ordinary one")
	}
}

func TestWithoutTheGroupMainBranchesStayAmongTheLocalOnes(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.SetOptions(Options{Grouping: Grouping{ByPath: true}})
	v.Render(workflowFixture(t))

	want := []string{"develop = origin", "feature", "  x = origin", "master = origin", "scratch"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local branches = %v, want %v", got, want)
	}
	local := rootByKey(t, v, tw.Tree, localGroupKey)
	if childByText(t, local, "master = origin").Icon == childByText(t, local, "scratch").Icon {
		t.Fatal("a main branch keeps its icon without the group")
	}
}

func TestTheMainGroupKeepsItsStateAcrossRenders(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.Render(workflowFixture(t))
	tw.Tree.SetSelectedItem(childByText(t, rootByKey(t, v, tw.Tree, mainGroupKey), "master = origin"))
	rootByKey(t, v, tw.Tree, mainGroupKey).Expanded = false

	v.Render(workflowFixture(t))

	main := rootByKey(t, v, tw.Tree, mainGroupKey)
	if main.Expanded {
		t.Fatal("the collapsed main group opened again")
	}
	if got := v.idByItem[tw.Tree.SelectedItem()]; got != refs.BranchName("master") {
		t.Fatalf("selected = %v, want master", got)
	}
}

func TestSelectingObsoleteBranchesLeavesMainBranchesAlone(t *testing.T) {
	v, _ := boundWithMainSection(t)
	s := obsoleteFixture(t)
	s.Current = "kept"
	v.Render(s)

	stale := v.SelectObsolete()

	if slices.Contains(stale, refs.BranchName("main")) {
		t.Fatalf("stale = %v, want main left alone", stale)
	}
	if !slices.Contains(stale, refs.BranchName("gone")) {
		t.Fatalf("stale = %v, want gone selected", stale)
	}
}

func TestAMainBranchIsNotRepeatedInAGitFlowSection(t *testing.T) {
	v, tw := boundWithMainSection(t)
	v.SetFlow(ops.DefaultFlowConfig(), true)
	s := Snapshot{
		Local: []Branch{
			{Name: refs.BranchName("release/current"), Target: oid(t, "11")},
			{Name: refs.BranchName("release/2.0"), Target: oid(t, "22")},
		},
		Remotes: []Remote{{
			Name: "origin",
			Head: refs.RemoteBranchName("origin", "release/current"),
			Branches: []Branch{
				{Name: refs.RemoteBranchName("origin", "release/current"), Target: oid(t, "11")},
			},
		}},
	}
	v.Render(s)

	want := []string{"Main", "  release/current = origin"}
	if got := sectionOutline(t, v, tw.Tree, mainGroupKey); !slices.Equal(got, want) {
		t.Fatalf("main group = %v, want %v", got, want)
	}
	want = []string{"Releases (1)", "  2.0"}
	if got := sectionOutline(t, v, tw.Tree, flowKey(ops.FlowKindRelease)); !slices.Equal(got, want) {
		t.Fatalf("releases section = %v, want %v", got, want)
	}
}
