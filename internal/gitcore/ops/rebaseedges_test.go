package ops

import (
	"errors"
	"testing"
)

func TestARebaseLooksPastMergesOnTheUpstream(t *testing.T) {
	tr := newTestRepo(t)
	_, upstream := tr.rebaseFork(false)
	tr.switchTo("main")
	tr.createBranch("side", upstream)
	tr.switchTo("side")
	tr.commitFiles("side", map[string]string{"s": "s\n"})
	tr.switchTo("main")
	tr.commitFiles("main again", map[string]string{"m": "m\n"})
	if _, err := tr.merge("side", MergeOptions{Mode: MergeNoFastForward, When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	tr.switchTo("topic")

	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || !result.Finished() || result.Applied != 3 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestRestoringTheHeadOfAnAbortedAmNeedsAReadableHead(t *testing.T) {
	tr := newTestRepo(t)
	before, _ := tr.stopGitAm()
	m, err := openMerger(t.Context(), tr.repo, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	tr.writeRawHead("garbage\n")

	if err := m.restoreRebaseHead(RebaseState{Applying: true, OrigHead: before}); err == nil {
		t.Fatal("HEAD was restored over an unreadable HEAD")
	}
}

func TestPlannedRebaseNeedsABornReadableHead(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.createBranch("upstream", tr.branchTarget("main"))

	tr.writeRawHead("ref: refs/heads/fresh\n")
	if _, err := PlannedRebase(t.Context(), tr.repo, "upstream"); !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("unborn: err = %v", err)
	}
	tr.writeRawHead("garbage\n")
	if _, err := PlannedRebase(t.Context(), tr.repo, "upstream"); err == nil {
		t.Fatal("a rebase was planned over an unreadable HEAD")
	}
}
