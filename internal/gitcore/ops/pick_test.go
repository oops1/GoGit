package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (r *testRepo) pickFork(conflict bool) hash.ObjectID {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f, "g": tenLines("g")})
	r.createBranch("feature", base)
	ours := map[string]string{"g": changeLine(tenLines("g"), 9, "OURS")}
	if conflict {
		ours = map[string]string{"f": changeLine(f, 4, "OURS")}
	}
	r.commitFiles("ours", ours)
	r.switchTo("feature")
	picked := r.commitWithAuthor("picked change\n\nwith a body", map[string]string{"f": changeLine(f, 4, "PICKED")})
	r.switchTo("main")
	return picked
}

func (r *testRepo) commitWithAuthor(message string, files map[string]string) hash.ObjectID {
	r.t.Helper()
	id := r.commitFiles(message, files)
	db := r.db()
	c, err := db.Commit(id)
	if err != nil {
		r.t.Fatal(err)
	}
	c.Author = object.Signature{Name: "Other", Email: "other@example.com", When: c.Author.When}
	rewritten, err := db.PutObject(c)
	if err != nil {
		r.t.Fatal(err)
	}
	store := r.refs()
	tx := store.Begin()
	branch, err := currentBranchRef(store)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := tx.Set(branch, rewritten); err != nil {
		r.t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatal(err)
	}
	return rewritten
}

func pickOptions() PickOptions { return PickOptions{When: mergeTime} }

func TestCherryPickCopiesAChangeAndKeepsItsAuthor(t *testing.T) {
	tr := newTestRepo(t)
	picked := tr.pickFork(false)
	head := tr.branchTarget("main")

	result, err := CherryPick(t.Context(), tr.repo, "feature", pickOptions())

	if err != nil || !result.Clean() || result.Picked != picked {
		t.Fatalf("result = %+v, %v", result, err)
	}
	c, err := tr.db().Commit(result.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if c.Author.Name != "Other" || c.Committer.Name != "ann" || c.Message != "picked change\n\nwith a body\n" || c.Parents[0] != head {
		t.Fatalf("commit = %+v", c)
	}
	if !strings.Contains(tr.readFile("f"), "PICKED") || tr.branchTarget("main") != result.Commit {
		t.Fatal("the change did not land on main")
	}
}

func TestRevertUndoesAChangeWithItsOwnMessage(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	change := tr.commitFiles("change", map[string]string{"f": changeLine(f, 4, "CHANGED")})

	result, err := Revert(t.Context(), tr.repo, "HEAD", pickOptions())

	if err != nil || !result.Clean() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	c, err := tr.db().Commit(result.Commit)
	if err != nil {
		t.Fatal(err)
	}
	want := "Revert \"change\"\n\nThis reverts commit " + change.String() + ".\n"
	if c.Message != want || c.Author.Name != "ann" || tr.readFile("f") != f {
		t.Fatalf("commit = %+v", c)
	}
}

func TestAConflictedPickWaitsForTheCommitThatKeepsTheAuthor(t *testing.T) {
	tr := newTestRepo(t)
	picked := tr.pickFork(true)

	result, err := CherryPick(t.Context(), tr.repo, "feature", pickOptions())

	if err != nil || result.Clean() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	state := tr.mergeState()
	if state.Operation() != OperationCherryPick || state.Picked != picked || !strings.HasPrefix(state.Message, "picked change") {
		t.Fatalf("state = %+v", state)
	}
	if text := tr.readFile("f"); !strings.Contains(text, ">>>>>>> "+abbreviate(picked)+" (picked change)\n") {
		t.Fatalf("file:\n%s", text)
	}
	tr.writeFile("f", "resolved\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	id, err := Commit(t.Context(), tr.repo, CommitOptions{Message: state.Message, When: mergeTime})
	if err != nil {
		t.Fatal(err)
	}
	c, err := tr.db().Commit(id)
	if err != nil || c.Author.Name != "Other" || len(c.Parents) != 1 || tr.mergeState().InProgress() {
		t.Fatalf("commit = %+v, %v", c, err)
	}
}

func TestAConflictedRevertCanBeAborted(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.commitFiles("base", map[string]string{"f": f})
	tr.commitFiles("change", map[string]string{"f": changeLine(f, 4, "CHANGED")})
	head := tr.commitFiles("again", map[string]string{"f": changeLine(f, 4, "AGAIN")})

	result, err := Revert(t.Context(), tr.repo, "HEAD~1", pickOptions())
	if err != nil || result.Clean() || tr.mergeState().Operation() != OperationRevert {
		t.Fatalf("result = %+v, %v", result, err)
	}

	if err := AbortOperation(t.Context(), tr.repo); err != nil {
		t.Fatal(err)
	}
	if tr.readFile("f") != changeLine(f, 4, "AGAIN") || tr.branchTarget("main") != head || tr.mergeState().InProgress() {
		t.Fatal("the abort did not restore HEAD")
	}
}

func TestPickingRefusesWhatItCannotDo(t *testing.T) {
	tr := newTestRepo(t)
	tr.pickFork(false)
	tr.switchTo("feature")
	if _, err := tr.merge("main", MergeOptions{Mode: MergeNoFastForward}); err != nil {
		t.Fatal(err)
	}
	tr.switchTo("main")

	if _, err := CherryPick(t.Context(), tr.repo, "feature", pickOptions()); !errors.Is(err, ErrPickMergeCommit) {
		t.Fatalf("merge commit: err = %v", err)
	}
	if _, err := CherryPick(t.Context(), tr.repo, "missing", pickOptions()); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("missing: err = %v", err)
	}
	if _, err := CherryPick(t.Context(), tr.repo, "main", pickOptions()); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("already there: err = %v", err)
	}
}

func TestAPickWaitsForTheMergeInProgress(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := CherryPick(t.Context(), tr.repo, "feature", pickOptions()); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("err = %v", err)
	}
}

func TestPickingARootCommitAddsItsFiles(t *testing.T) {
	tr := newTestRepo(t)
	root := tr.commitFiles("root", map[string]string{"r": "root\n"})
	tr.writeRawHead("ref: refs/heads/other\n")
	tr.saveIndex(index.New(index.Version2))
	tr.remove("r")
	tr.commitFiles("other", map[string]string{"o": "other\n"})

	result, err := CherryPick(t.Context(), tr.repo, root.String(), pickOptions())

	if err != nil || result.Commit.IsZero() || tr.readFile("r") != "root\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestPickingIntoAnUnbornBranchIsRefused(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.writeRawHead("ref: refs/heads/fresh\n")

	if _, err := CherryPick(t.Context(), tr.repo, "main", pickOptions()); !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("err = %v", err)
	}
}

func TestPickingNeedsAnIdentity(t *testing.T) {
	tr := newTestRepoNoIdentity(t)

	if _, err := CherryPick(t.Context(), tr.repo, "main", pickOptions()); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v", err)
	}
	if _, err := CherryPick(t.Context(), newBareTestRepo(t).repo, "main", pickOptions()); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("bare: err = %v", err)
	}
}

func TestAGarbledPickHeadIsReported(t *testing.T) {
	tr := newTestRepo(t)
	for _, name := range []string{pickHeadFile, revertFile} {
		if err := tr.repo.Root().WriteFile(name, []byte("not a hash\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadMergeState(tr.repo); err == nil {
			t.Fatalf("%s: a garbled head was accepted", name)
		}
		if err := tr.repo.Root().Remove(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestACommitAfterAPickFailsWhenThePickedCommitIsGone(t *testing.T) {
	tr := newTestRepo(t)
	before := tr.commitFiles("base", map[string]string{"f": "f\n"})
	missing := bogusObjectID(t, tr.repo.ObjectFormat)
	if err := tr.repo.Root().WriteFile(pickHeadFile, []byte(missing.String()+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	tr.writeFile("f", "changed\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "x"}); err == nil {
		t.Fatal("a commit borrowed the author of a missing commit")
	}
	if tr.branchTarget("main") != before {
		t.Fatal("main moved on a failed commit")
	}
}

func TestPickingStopsOnAnUnreadableHead(t *testing.T) {
	tr := newTestRepo(t)
	picked := tr.pickFork(false)
	tr.writeRawHead("garbage\n")

	if _, err := CherryPick(t.Context(), tr.repo, picked.String(), pickOptions()); err == nil {
		t.Fatal("a pick ran over an unreadable HEAD")
	}
}
