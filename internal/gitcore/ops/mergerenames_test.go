package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/merge"
)

func TestTheMergeRenameLimitFallsBackToTheDiffLimitAndThenToGits(t *testing.T) {
	tr := newTestRepo(t)
	if got := mergeRenameLimit(tr.reopen()); got != merge.DefaultRenameLimit {
		t.Fatalf("default limit = %d", got)
	}
	tr.appendConfig("[diff]\n\trenameLimit = 50\n")
	if got := mergeRenameLimit(tr.reopen()); got != 50 {
		t.Fatalf("limit with diff.renameLimit = %d", got)
	}
	tr.appendConfig("[merge]\n\trenameLimit = 20\n")
	if got := mergeRenameLimit(tr.reopen()); got != 20 {
		t.Fatalf("limit with merge.renameLimit = %d", got)
	}
}

func TestDirectoryRenamesFollowTheMergeConfig(t *testing.T) {
	for text, want := range map[string]merge.DirectoryRenames{
		"": merge.DirectoryRenamesConflict,
		"[merge]\n\tdirectoryRenames = conflict\n": merge.DirectoryRenamesConflict,
		"[merge]\n\tdirectoryRenames = true\n":     merge.DirectoryRenamesApply,
		"[merge]\n\tdirectoryRenames = false\n":    merge.DirectoryRenamesOff,
		"[merge]\n\tdirectoryRenames = maybe\n":    merge.DirectoryRenamesConflict,
	} {
		r := newTestRepo(t)
		r.appendConfig(text)
		if got := directoryRenamesMode(r.reopen()); got != want {
			t.Errorf("config %q: mode %d, want %d", text, got, want)
		}
	}
}

func TestAMergePastTheRenameLimitWarnsInsteadOfPairingFiles(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[merge]\n\trenameLimit = 1\n")
	tr.repo = tr.reopen()
	tr.fork(
		map[string]string{"f": "", "g": "", "f2": tenLines("f") + "ours\n", "g2": tenLines("g") + "ours\n"},
		map[string]string{"f": changeLine(tenLines("f"), 0, "THEIRS"), "g": changeLine(tenLines("g"), 0, "THEIRS")},
	)

	result, err := tr.merge("feature", MergeOptions{})

	want := []merge.Warning{{Kind: merge.WarningRenameLimit, Needed: 2}}
	if err != nil || !slices.Equal(result.Warnings, want) || !slices.Equal(result.Conflicts, []string{"f", "g"}) {
		t.Fatalf("result = %+v, %v; want modify/delete conflicts and a rename limit warning", result, err)
	}
}
