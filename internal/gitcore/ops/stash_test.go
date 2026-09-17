package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func stashChanges(tr *testRepo) {
	tr.t.Helper()
	tr.commitFiles("base\n\nbody", map[string]string{"a": "a\n", "b": "b\n", "c": "c\n", "dir/d": "d\n"})
	tr.writeFile("a", "a2\n")
	tr.writeFile("b", "b2\n")
	tr.writeFile("new", "new\n")
	if err := Stage(tr.t.Context(), tr.repo, []string{"b", "new"}, StageOptions{}); err != nil {
		tr.t.Fatalf("Stage returned error %v", err)
	}
	tr.remove("c")
}

func (r *testRepo) stash(opts StashOptions) hash.ObjectID {
	r.t.Helper()
	if opts.When.IsZero() {
		opts.When = mergeTime
	}
	id, err := StashPush(r.t.Context(), r.repo, opts)
	if err != nil {
		r.t.Fatalf("StashPush returned error %v", err)
	}
	return id
}

func (r *testRepo) stashes() []StashEntry {
	r.t.Helper()
	entries, err := StashList(r.t.Context(), r.repo)
	if err != nil {
		r.t.Fatalf("StashList returned error %v", err)
	}
	return entries
}

func (r *testRepo) breakCacheTree() {
	r.t.Helper()
	idx := r.index()
	sub := idx.CacheTree.Find("dir")
	if sub == nil {
		r.t.Fatal("dir cache subtree was not built")
	}
	sub.EntryCount = 99
	idx.CacheTree.Invalidate()
	r.saveIndex(idx)
}

func TestStashPushSavesTheChangesAndApplyBringsThemBack(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)

	id := tr.stash(StashOptions{})

	if tr.readFile("a") != "a\n" || tr.readFile("b") != "b\n" || tr.exists("new") || !tr.exists("c") {
		t.Fatal("StashPush left local changes in the working tree")
	}
	entries := tr.stashes()
	if len(entries) != 1 || entries[0].Commit != id || entries[0].Selector() != "stash@{0}" {
		t.Fatalf("StashList = %+v", entries)
	}
	if want := "WIP on main: " + abbreviate(tr.branchTarget("main")) + " base"; entries[0].Message != want {
		t.Fatalf("stash message = %q, want %q", entries[0].Message, want)
	}

	result, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{})
	if err != nil || !result.Clean() || result.Dropped {
		t.Fatalf("StashApply = %+v, %v", result, err)
	}
	if tr.readFile("a") != "a2\n" || tr.readFile("b") != "b2\n" || tr.readFile("new") != "new\n" || tr.exists("c") {
		t.Fatal("StashApply did not restore the working tree")
	}
	idx := tr.index()
	if _, ok := entryOf(t, idx, "new"); !ok {
		t.Fatal("StashApply unstaged a new file")
	}
	if _, ok := entryOf(t, idx, "c"); !ok {
		t.Fatal("StashApply staged a deletion")
	}
	if len(tr.stashes()) != 1 {
		t.Fatal("StashApply dropped the entry")
	}
}

func TestStashPushNamesTheStashAfterTheMessageAndDetachedHead(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{Message: "keep"})
	head := tr.branchTarget("main")
	tr.switchTo(head.String())
	tr.writeFile("a", "detached\n")
	tr.stash(StashOptions{})

	entries := tr.stashes()
	want := []string{"WIP on (no branch): " + abbreviate(head) + " base", "On main: keep"}
	if len(entries) != 2 || entries[0].Message != want[0] || entries[1].Message != want[1] || entries[1].Index != 1 {
		t.Fatalf("StashList = %+v, want %q", entries, want)
	}
}

func TestStashPushKeepsFilesRemovedOnlyFromTheIndex(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n", "c": "c\n"})
	idx := tr.index()
	idx.Remove("c")
	tr.saveIndex(idx)

	id := tr.stash(StashOptions{})

	work, err := commitTreeEntries(tr.db(), id)
	if err != nil {
		t.Fatalf("commitTreeEntries returned error %v", err)
	}
	if _, ok := work["c"]; !ok {
		t.Fatal("the work tree of the stash lost a file that is still on disk")
	}
	stash, err := tr.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	staged, err := commitTreeEntries(tr.db(), stash.Parents[1])
	if err != nil {
		t.Fatalf("commitTreeEntries returned error %v", err)
	}
	if _, ok := staged["c"]; ok {
		t.Fatal("the index tree of the stash kept an unstaged file")
	}
}

func TestStashPushRefusesWhenThereIsNothingToSave(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("untracked", "u\n")
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{}); !errors.Is(err, ErrNothingToStash) {
		t.Fatalf("StashPush returned %v, want ErrNothingToStash", err)
	}
}

func TestStashPushRefusesAnUnbornHeadAndAMissingIdentity(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a", "a\n")
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{}); !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("unborn HEAD returned %v", err)
	}
	anonymous := newTestRepoNoIdentity(t)
	stashChanges(anonymous)
	if _, err := StashPush(t.Context(), anonymous.repo, StashOptions{}); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("missing identity returned %v", err)
	}
}

func TestStashRefusesAnIndexWithConflicts(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.writeFile("keep", "changed\n")
	tr.stash(StashOptions{})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatalf("Merge returned error %v", err)
	}
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{}); !errors.Is(err, ErrUnmergedPaths) {
		t.Fatalf("StashPush returned %v", err)
	}
	if _, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{}); !errors.Is(err, ErrUnmergedPaths) {
		t.Fatalf("StashApply returned %v", err)
	}
}

func TestStashReportsAnUnreadableIndex(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{})
	tr.corruptIndexFile()
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{}); err == nil {
		t.Fatal("StashPush accepted a corrupt index")
	}
	if _, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{}); err == nil {
		t.Fatal("StashApply accepted a corrupt index")
	}
}

func TestStashReportsAnIndexThatCannotBeWrittenAsATree(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.breakCacheTree()
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{}); !errors.Is(err, index.ErrMalformed) {
		t.Fatalf("StashPush returned %v", err)
	}

	clean := newTestRepo(t)
	stashChanges(clean)
	clean.stash(StashOptions{})
	clean.breakCacheTree()
	if _, err := StashApply(t.Context(), clean.repo, 0, StashApplyOptions{}); !errors.Is(err, index.ErrMalformed) {
		t.Fatalf("StashApply returned %v", err)
	}
}

func TestStashApplyRefusesToOverwriteLocalChanges(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{})
	tr.writeFile("a", "dirty\n")
	var overwrite *OverwriteError
	if _, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{}); !errors.As(err, &overwrite) || !slices.Equal(overwrite.Paths, []string{"a"}) {
		t.Fatalf("StashApply returned %v", err)
	}
	if tr.readFile("a") != "dirty\n" {
		t.Fatal("StashApply overwrote a local change")
	}
}

func TestStashPopDropsOnlyACleanApplication(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{})
	result, err := StashPop(t.Context(), tr.repo, 0, StashApplyOptions{})
	if err != nil || !result.Dropped || len(tr.stashes()) != 0 {
		t.Fatalf("StashPop = %+v, %v", result, err)
	}

	tr.stash(StashOptions{})
	tr.commitFiles("upstream", map[string]string{"a": "upstream\n"})
	result, err = StashPop(t.Context(), tr.repo, 0, StashApplyOptions{})
	if err != nil || result.Dropped || !slices.Equal(result.Conflicts, []string{"a"}) || len(tr.stashes()) != 1 {
		t.Fatalf("conflicting StashPop = %+v, %v", result, err)
	}
}

func TestStashPopReportsADropThatFailsAfterApplying(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{})
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), "refs", "stash.lock"), nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	result, err := StashPop(t.Context(), tr.repo, 0, StashApplyOptions{})
	if !errors.Is(err, refs.ErrLocked) || result.Dropped || tr.readFile("a") != "a2\n" {
		t.Fatalf("StashPop = %+v, %v", result, err)
	}
}

func TestStashDropRemovesTheChosenEntry(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{Message: "first"})
	tr.writeFile("a", "second\n")
	tr.stash(StashOptions{Message: "second"})

	if err := StashDrop(t.Context(), tr.repo, 1); err != nil {
		t.Fatalf("StashDrop returned error %v", err)
	}
	entries := tr.stashes()
	if len(entries) != 1 || entries[0].Message != "On main: second" {
		t.Fatalf("StashList = %+v", entries)
	}
}

func TestStashOperationsRejectMissingEntries(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	if _, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{}); !errors.Is(err, ErrStashNotFound) {
		t.Fatalf("StashApply returned %v", err)
	}
	tr.stash(StashOptions{})
	if _, err := StashPop(t.Context(), tr.repo, -1, StashApplyOptions{}); !errors.Is(err, ErrStashNotFound) {
		t.Fatalf("StashPop returned %v", err)
	}
	if err := StashDrop(t.Context(), tr.repo, 3); !errors.Is(err, ErrStashNotFound) {
		t.Fatalf("StashDrop returned %v", err)
	}
}

func TestStashApplyRejectsACommitThatIsNotAStash(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"a": "a\n"})
	store := tr.refs()
	tx := store.Begin()
	tx.SetMessage("not a stash")
	if err := tx.Set(refs.StashName, head); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if _, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{}); !errors.Is(err, ErrNotAStash) {
		t.Fatalf("StashApply returned %v", err)
	}
}

func TestStashReportsAMalformedReflog(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{})
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), "logs", "refs", "stash"), []byte("garbage\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if _, err := StashList(t.Context(), tr.repo); !errors.Is(err, refs.ErrMalformedReflog) {
		t.Fatalf("StashList returned %v", err)
	}
	if _, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{}); !errors.Is(err, refs.ErrMalformedReflog) {
		t.Fatalf("StashApply returned %v", err)
	}
	if err := StashDrop(t.Context(), tr.repo, 0); !errors.Is(err, refs.ErrMalformedReflog) {
		t.Fatalf("StashDrop returned %v", err)
	}
}

func TestStashOperationsNeedAWorkingTreeAndALiveContext(t *testing.T) {
	bare := newBareTestRepo(t)
	if _, err := StashPush(t.Context(), bare.repo, StashOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("StashPush returned %v", err)
	}
	if _, err := StashApply(t.Context(), bare.repo, 0, StashApplyOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("StashApply returned %v", err)
	}
	if _, err := StashPop(t.Context(), bare.repo, 0, StashApplyOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("StashPop returned %v", err)
	}

	tr := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := StashList(ctx, tr.repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("StashList returned %v", err)
	}
	if err := StashDrop(ctx, tr.repo, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("StashDrop returned %v", err)
	}
}

func TestStashSubjectJoinsTheFirstParagraph(t *testing.T) {
	cases := map[string]string{
		"one\ntwo  \n\nthree\n": "one two",
		"\n\nlead\n":            "lead",
		"":                      "",
	}
	for message, want := range cases {
		if got := stashSubject(message); got != want {
			t.Fatalf("stashSubject(%q) = %q, want %q", message, got, want)
		}
	}
}

func switchMergingRepo(tr *testRepo) {
	tr.t.Helper()
	base := tr.commitFiles("base", map[string]string{"a": "a\n", "b": "b\n", "f": tenLines("f")})
	tr.createBranch("feature", base)
	tr.switchTo("feature")
	tr.commitFiles("feature", map[string]string{"b": "feature\n", "f": changeLine(tenLines("f"), 0, "FEATURE")})
	tr.switchTo("main")
}

func TestSwitchMergingSwitchesPlainlyWithoutChanges(t *testing.T) {
	tr := newTestRepo(t)
	switchMergingRepo(tr)
	result, err := SwitchMerging(t.Context(), tr.repo, "feature")
	if err != nil || result.Stashed || !result.Clean() {
		t.Fatalf("SwitchMerging = %+v, %v", result, err)
	}
	if target, _ := tr.headSymbolicTarget(); target != refs.BranchName("feature") {
		t.Fatalf("HEAD points to %s", target)
	}
}

func TestSwitchMergingCarriesChangesThatMergeCleanly(t *testing.T) {
	tr := newTestRepo(t)
	switchMergingRepo(tr)
	tr.writeFile("f", changeLine(tenLines("f"), 9, "LOCAL"))

	result, err := SwitchMerging(t.Context(), tr.repo, "feature")

	if err != nil || !result.Stashed || !result.Clean() {
		t.Fatalf("SwitchMerging = %+v, %v", result, err)
	}
	want := changeLine(changeLine(tenLines("f"), 0, "FEATURE"), 9, "LOCAL")
	if tr.readFile("f") != want || len(tr.stashes()) != 0 {
		t.Fatalf("f = %q with %d stashes left", tr.readFile("f"), len(tr.stashes()))
	}
}

func TestSwitchMergingKeepsTheStashWhenChangesConflict(t *testing.T) {
	tr := newTestRepo(t)
	switchMergingRepo(tr)
	tr.writeFile("b", "local\n")

	result, err := SwitchMerging(t.Context(), tr.repo, "feature")

	if err != nil || !result.Stashed || !slices.Equal(result.Conflicts, []string{"b"}) || len(tr.stashes()) != 1 {
		t.Fatalf("SwitchMerging = %+v, %v", result, err)
	}
}

func TestSwitchMergingRestoresTheChangesWhenTheSwitchFails(t *testing.T) {
	tr := newTestRepo(t)
	switchMergingRepo(tr)
	tr.writeFile("b", "local\n")

	result, err := SwitchMerging(t.Context(), tr.repo, "missing")

	if !errors.Is(err, ErrTargetNotFound) || !result.Stashed {
		t.Fatalf("SwitchMerging = %+v, %v", result, err)
	}
	if tr.readFile("b") != "local\n" || len(tr.stashes()) != 0 {
		t.Fatal("SwitchMerging did not restore the local changes")
	}

	anonymous := newTestRepoNoIdentity(t)
	stashChanges(anonymous)
	if _, err := SwitchMerging(t.Context(), anonymous.repo, "main"); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("SwitchMerging without identity returned %v", err)
	}
}
