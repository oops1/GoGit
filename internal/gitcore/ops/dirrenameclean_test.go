package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/merge"
)

func TestADirectoryRenameSplitStopsTheMergeButNotAVirtualBase(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"lib/a": tenLines("a"), "lib/b": tenLines("b")})
	tr.createBranch("feature", base)
	ours := tr.commitFiles("ours", map[string]string{"lib/a": "", "lib/b": "", "x/a": tenLines("a"), "y/b": tenLines("b")})
	tr.switchTo("feature")
	theirs := tr.commitFiles("theirs", map[string]string{"lib/new": "new\n"})
	tr.switchTo("main")
	m, err := openMerger(t.Context(), tr.reopen(), MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	trees := make([]hash.ObjectID, 3)
	for at, commit := range []hash.ObjectID{base, ours, theirs} {
		if trees[at], err = m.treeOf(commit); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := m.mergeTrees(trees[0], trees[1], trees[2], merge.Labels{}, 1); err != nil || len(m.warnings) != 0 {
		t.Fatalf("virtual base warnings = %+v, %v", m.warnings, err)
	}
	if _, err := m.mergeTrees(trees[0], trees[1], trees[2], merge.Labels{}, 0); err != nil || !slices.ContainsFunc(m.warnings, merge.Warning.Unclean) {
		t.Fatalf("outer merge warnings = %+v, %v", m.warnings, err)
	}
	result, err := tr.merge("feature", MergeOptions{})
	if err != nil || result.Clean() || result.Committed || !tr.mergeState().InProgress() {
		t.Fatalf("result = %+v, %v; want the merge stopped before committing", result, err)
	}
}
