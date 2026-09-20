package branches

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/style"
)

func TestDivergencePairsSkipsLocalBranchesWithoutUpstream(t *testing.T) {
	snap := Snapshot{Local: []Branch{{Name: refs.BranchName("main"), Target: oid(t, "11")}}}

	pairs := DivergencePairs(snap)

	if len(pairs) != 0 {
		t.Fatalf("pairs = %+v, want none for a branch without an upstream", pairs)
	}
}

func TestDivergencePairsSkipsAnUpstreamThatIsNotAKnownRemoteBranch(t *testing.T) {
	snap := Snapshot{Local: []Branch{{
		Name:     refs.BranchName("main"),
		Target:   oid(t, "11"),
		Upstream: refs.RemoteBranchName("origin", "main"),
	}}}

	pairs := DivergencePairs(snap)

	if len(pairs) != 0 {
		t.Fatalf("pairs = %+v, want none when the tracking ref is missing", pairs)
	}
}

func TestDivergencePairsPairsLocalAndRemoteCommits(t *testing.T) {
	snap := Snapshot{
		Local: []Branch{
			{Name: refs.BranchName("main"), Target: oid(t, "11"), Upstream: refs.RemoteBranchName("origin", "main")},
			{Name: refs.BranchName("scratch"), Target: oid(t, "22")},
		},
		Remotes: []Remote{{
			Name:     "origin",
			Branches: []Branch{{Name: refs.RemoteBranchName("origin", "main"), Target: oid(t, "33")}},
		}},
	}

	pairs := DivergencePairs(snap)

	if len(pairs) != 1 {
		t.Fatalf("pairs = %+v, want exactly one", pairs)
	}
	want := repo.BranchPair{Name: refs.BranchName("main"), Local: oid(t, "11"), Remote: oid(t, "33")}
	if pairs[0] != want {
		t.Fatalf("pairs[0] = %+v, want %+v", pairs[0], want)
	}
}

func TestDivergenceSuffixFormatsAheadBehindAndBoth(t *testing.T) {
	cases := []struct {
		name string
		in   repo.Divergence
		want string
	}{
		{"none", repo.Divergence{}, ""},
		{"ahead", repo.Divergence{Ahead: 3}, " ↑3"},
		{"behind", repo.Divergence{Behind: 1}, " ↓1"},
		{"both", repo.Divergence{Ahead: 3, Behind: 1}, " ↑3 ↓1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := divergenceSuffix(c.in); got != c.want {
				t.Fatalf("divergenceSuffix(%+v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestViewAppendsDivergenceSuffixToADivergedLocalBranch(t *testing.T) {
	v, tw := bound(t)
	snap := Snapshot{
		Current: "main",
		Local: []Branch{
			{Name: refs.BranchName("main"), Target: oid(t, "11")},
			{Name: refs.BranchName("develop"), Target: oid(t, "22")},
		},
	}
	v.Render(snap)

	v.SetDivergence(map[refs.Name]repo.Divergence{
		refs.BranchName("develop"): {Ahead: 3, Behind: 1},
	})

	local, _, _ := roots(t, tw)
	item := findChild(t, local, "develop ↑3 ↓1")
	if item.Foreground != v.secondary {
		t.Fatalf("Foreground = %v, want the muted secondary colour %v", item.Foreground, v.secondary)
	}
	main := findChild(t, local, "main")
	if main.Foreground.A != 0 {
		t.Fatalf("a branch without divergence must keep the theme colour, got %v", main.Foreground)
	}
}

func TestViewClearsDivergenceSuffixWhenBranchesMatchAgain(t *testing.T) {
	v, tw := bound(t)
	snap := Snapshot{Local: []Branch{{Name: refs.BranchName("develop"), Target: oid(t, "11")}}}
	v.Render(snap)
	v.SetDivergence(map[refs.Name]repo.Divergence{refs.BranchName("develop"): {Ahead: 1}})

	v.SetDivergence(nil)

	local, _, _ := roots(t, tw)
	item := findChild(t, local, "develop")
	if item.Foreground.A != 0 {
		t.Fatalf("Foreground = %v, want the theme colour once divergence clears", item.Foreground)
	}
}

func TestRestyleChangesTheMutedColourAndRerendersExistingDivergence(t *testing.T) {
	v, tw := bound(t)
	snap := Snapshot{Local: []Branch{{Name: refs.BranchName("develop"), Target: oid(t, "11")}}}
	v.Render(snap)
	v.SetDivergence(map[refs.Name]repo.Divergence{refs.BranchName("develop"): {Ahead: 1}})

	theme := &widget.Theme{SecondaryText: style.Of(widget.CurrentTheme()).Text}
	v.Restyle(theme)

	local, _, _ := roots(t, tw)
	item := findChild(t, local, "develop ↑1")
	if item.Foreground != theme.SecondaryText {
		t.Fatalf("Foreground = %v, want the restyled secondary colour %v", item.Foreground, theme.SecondaryText)
	}
}
