package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/merge"
)

func (r *testRepo) splitDirectoryHistory() {
	r.t.Helper()
	base := r.commitFiles("base", map[string]string{"lib/a": tenLines("a"), "lib/b": tenLines("b")})
	r.createBranch("feature", base)
	r.commitFiles("ours", map[string]string{"lib/a": "", "lib/b": "", "x/a": tenLines("a"), "y/b": tenLines("b")})
	r.switchTo("feature")
	r.commitFiles("theirs", map[string]string{"lib/new": "new\n"})
}

func TestAPickIntoADirectoryRenameSplitStopsWithoutCommitting(t *testing.T) {
	tr := newTestRepo(t)
	tr.splitDirectoryHistory()
	tr.switchTo("main")
	head := tr.branchTarget("main")

	result, err := CherryPick(t.Context(), tr.repo, "feature", PickOptions{When: mergeTime})

	if err != nil || result.Clean() || !result.Commit.IsZero() || tr.branchTarget("main") != head || !tr.mergeState().InProgress() {
		t.Fatalf("result = %+v, %v; want the pick stopped", result, err)
	}
	if !slices.ContainsFunc(result.Warnings, merge.Warning.Unclean) || len(result.Conflicts) != 0 {
		t.Fatalf("warnings = %+v, conflicts = %v", result.Warnings, result.Conflicts)
	}
}

func TestARebaseOverADirectoryRenameSplitStopsAtThePick(t *testing.T) {
	tr := newTestRepo(t)
	tr.splitDirectoryHistory()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime})

	if err != nil || result.Finished() || !result.Conflicted() || len(result.Conflicts) != 0 {
		t.Fatalf("result = %+v, %v; want the rebase stopped on an unclean merge", result, err)
	}
	if (RebaseResult{}).Conflicted() || !(RebaseResult{Conflicts: []string{"f"}}).Conflicted() {
		t.Fatal("Conflicted must follow the conflicts too")
	}
}

func TestAStashAppliedOverADirectoryRenameSplitKeepsTheIndexAndTheStash(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"lib/a": tenLines("a"), "lib/b": tenLines("b")})
	tr.writeFile("lib/new", "new\n")
	if err := Stage(t.Context(), tr.repo, []string{"lib/new"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{}); err != nil {
		t.Fatal(err)
	}
	tr.commitFiles("ours", map[string]string{"lib/a": "", "lib/b": "", "x/a": tenLines("a"), "y/b": tenLines("b")})

	result, err := StashPop(t.Context(), tr.repo, 0, StashApplyOptions{})

	if err != nil || result.Clean() || result.Dropped {
		t.Fatalf("result = %+v, %v; want the stash kept after an unclean apply", result, err)
	}
	if !slices.Contains(tr.stageEntries("lib/new"), 0) {
		t.Fatalf("lib/new stages = %v, want it left staged", tr.stageEntries("lib/new"))
	}
}
