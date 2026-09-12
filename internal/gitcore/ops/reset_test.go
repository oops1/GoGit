package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func resetOptions(mode ResetMode, paths ...string) ResetOptions {
	return ResetOptions{Mode: mode, Paths: paths, When: mergeTime}
}

func (r *testRepo) resetHistory() (hash.ObjectID, hash.ObjectID) {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
	next := r.commitFiles("next", map[string]string{"f": changeLine(f, 1, "NEXT"), "added": "added\n"})
	return base, next
}

func (r *testRepo) reset(target string, opts ResetOptions) (ResetResult, error) {
	r.t.Helper()
	return Reset(r.t.Context(), r.repo, target, opts)
}

func (r *testRepo) stagedID(path string) hash.ObjectID {
	r.t.Helper()
	entry, ok := r.index().Get(path, index.StageMerged)
	if !ok {
		return hash.Zero
	}
	return entry.ID
}

func TestASoftResetMovesOnlyTheBranch(t *testing.T) {
	tr := newTestRepo(t)
	base, next := tr.resetHistory()

	result, err := tr.reset("HEAD~1", resetOptions(ResetSoft))
	if err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if result.Old != next || result.New != base || tr.branchTarget("main") != base {
		t.Fatalf("result = %+v", result)
	}
	if !tr.exists("added") || tr.stagedID("added").IsZero() {
		t.Fatal("a soft reset touched the index or the working tree")
	}
}

func TestAMixedResetRewindsTheIndexOnly(t *testing.T) {
	tr := newTestRepo(t)
	base, _ := tr.resetHistory()

	if _, err := tr.reset("HEAD~1", resetOptions(ResetMixed)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if tr.branchTarget("main") != base {
		t.Fatal("the branch did not move")
	}
	if !tr.exists("added") || !tr.stagedID("added").IsZero() {
		t.Fatal("a mixed reset kept the addition staged")
	}
	if tr.readFile("f") != changeLine(tenLines("f"), 1, "NEXT") {
		t.Fatal("a mixed reset changed the working tree")
	}
}

func TestAHardResetRestoresTheWorkingTree(t *testing.T) {
	tr := newTestRepo(t)
	base, _ := tr.resetHistory()
	tr.writeFile("f", "dirty\n")
	tr.writeFile("spare", "spare\n")

	if _, err := tr.reset("HEAD~1", resetOptions(ResetHard)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if tr.branchTarget("main") != base || tr.readFile("f") != tenLines("f") {
		t.Fatal("a hard reset left the working tree behind")
	}
	if tr.exists("added") || !tr.exists("spare") {
		t.Fatal("a hard reset removed the wrong files")
	}
}

func TestAHardResetToHeadDropsLocalChanges(t *testing.T) {
	tr := newTestRepo(t)
	_, next := tr.resetHistory()
	tr.writeFile("f", "dirty\n")
	tr.remove("keep")

	if _, err := tr.reset("", resetOptions(ResetHard)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if tr.branchTarget("main") != next {
		t.Fatal("a reset to HEAD moved the branch")
	}
	if tr.readFile("f") != changeLine(tenLines("f"), 1, "NEXT") || tr.readFile("keep") != "keep\n" {
		t.Fatal("the working tree was not restored")
	}
}

func TestAResetRecordsTheOldHeadAndItsReflog(t *testing.T) {
	tr := newTestRepo(t)
	_, next := tr.resetHistory()

	if _, err := tr.reset("HEAD~1", resetOptions(ResetMixed)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if got := tr.readFile(".git/ORIG_HEAD"); got != next.String()+"\n" {
		t.Fatalf("ORIG_HEAD = %q", got)
	}
	last, err := tr.refs().ReflogLast(refs.HEAD)
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if last.Message != resetNotePrefix+"HEAD~1" {
		t.Fatalf("reflog = %q", last.Message)
	}
}

func TestAResetOfPathsLeavesTheBranchAlone(t *testing.T) {
	tr := newTestRepo(t)
	_, next := tr.resetHistory()

	if _, err := tr.reset("HEAD~1", resetOptions(ResetMixed, "added")); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if tr.branchTarget("main") != next || !tr.exists("added") {
		t.Fatal("a path reset moved the branch or the file")
	}
	if !tr.stagedID("added").IsZero() {
		t.Fatal("the addition stayed staged")
	}
}

func TestAResetOfPathsClearsConflictStages(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if _, err := tr.reset("HEAD", resetOptions(ResetMixed, "f")); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if stages := tr.stageEntries("f"); !slices.Equal(stages, []index.Stage{index.StageMerged}) {
		t.Fatalf("stages = %v", stages)
	}
	if tr.mergeState().Operation() != OperationMerge {
		t.Fatal("a path reset cleared the merge state")
	}
}

func TestAResetClearsTheMergeState(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if _, err := tr.reset("HEAD", resetOptions(ResetHard)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if tr.mergeState().InProgress() {
		t.Fatal("the merge state survived the reset")
	}
}

func TestASoftResetIsRefusedWhileMerging(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if _, err := tr.reset("HEAD", resetOptions(ResetSoft)); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("err = %v", err)
	}
}

func TestAResetOfPathsNeedsTheMixedMode(t *testing.T) {
	tr := newTestRepo(t)
	tr.resetHistory()

	for _, mode := range []ResetMode{ResetSoft, ResetHard} {
		if _, err := tr.reset("HEAD", resetOptions(mode, "f")); !errors.Is(err, ErrResetPathsWithMode) {
			t.Fatalf("mode %d: err = %v", mode, err)
		}
	}
}

func TestAResetOfAnUnknownTargetFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.resetHistory()

	if _, err := tr.reset("nope", resetOptions(ResetMixed)); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAResetOfAPathOutsideTheRepositoryFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.resetHistory()

	if _, err := tr.reset("HEAD", resetOptions(ResetMixed, "../outside")); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestAResetNeedsAWorkingTree(t *testing.T) {
	tr := newBareTestRepo(t)

	if _, err := tr.reset("HEAD", resetOptions(ResetMixed)); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v", err)
	}
}

func TestAResetOfAnUnbornBranchStartsIt(t *testing.T) {
	tr := newTestRepo(t)
	base, _ := tr.resetHistory()
	tr.writeRawHead("ref: refs/heads/unborn\n")
	tr.repo = tr.reopen()

	result, err := tr.reset(base.String(), resetOptions(ResetMixed))
	if err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if !result.Old.IsZero() || result.New != base || tr.branchTarget("unborn") != base {
		t.Fatalf("result = %+v", result)
	}
	if tr.exists(".git/ORIG_HEAD") {
		t.Fatal("an unborn branch left ORIG_HEAD behind")
	}
}
