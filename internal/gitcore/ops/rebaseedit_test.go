package ops

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func (r *testRepo) stopAtAnEdit() ([]RebaseResult, error) {
	r.t.Helper()
	commits := r.threeOnTopic()
	result, err := Rebase(r.t.Context(), r.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionEdit, commits[0], actionPick, commits[1]),
	})
	return []RebaseResult{result}, err
}

func TestContinuingAnUntouchedEditKeepsThePickedCommit(t *testing.T) {
	tr := newTestRepo(t)
	stops, err := tr.stopAtAnEdit()
	if err != nil {
		t.Fatal(err)
	}

	done, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime.Add(time.Hour)})

	if err != nil || !done.Finished() {
		t.Fatalf("continue = %+v, %v", done, err)
	}
	if history := tr.linearHistory(tr.branchTarget("topic"), 2); history[0].Parents[0] != stops[0].Amend {
		t.Fatalf("the edited commit was rewritten: %+v", history)
	}
}

func TestContinuingAnEditWithANewMessageRewordsIt(t *testing.T) {
	tr := newTestRepo(t)
	if _, err := tr.stopAtAnEdit(); err != nil {
		t.Fatal(err)
	}

	if _, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime, Message: "topic a reworded"}); err != nil {
		t.Fatalf("ContinueRebase returned error %v", err)
	}

	if got := tr.subjects(3); !slices.Equal(got, []string{"topic b", "topic a reworded", "main f"}) {
		t.Fatalf("history = %v", got)
	}
}

func TestContinuingAnEditKeepsTheCommitAmendedDuringTheStop(t *testing.T) {
	tr := newTestRepo(t)
	if _, err := tr.stopAtAnEdit(); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "amended by hand", Amend: true, AllowEmpty: true, When: mergeTime}); err != nil {
		t.Fatal(err)
	}

	if _, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime, Message: "topic a"}); err != nil {
		t.Fatalf("ContinueRebase returned error %v", err)
	}

	if got := tr.subjects(3); !slices.Equal(got, []string{"topic b", "amended by hand", "main f"}) {
		t.Fatalf("history = %v", got)
	}
}

func TestContinuingAnEditRefusesChangesStagedAfterHeadMoved(t *testing.T) {
	tr := newTestRepo(t)
	if _, err := tr.stopAtAnEdit(); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "amended by hand", Amend: true, AllowEmpty: true, When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	tr.writeFile("a", "staged after the amend\n")
	if err := Stage(t.Context(), tr.repo, []string{"a"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime}); !errors.Is(err, ErrRebaseUncommittedChanges) {
		t.Fatalf("err = %v", err)
	}
	if !tr.rebaseState().Amending() {
		t.Fatal("the refused continue left the stop")
	}
}

func TestContinuingAnEditFailsWhenHeadIsNotACommit(t *testing.T) {
	tr := newTestRepo(t)
	if _, err := tr.stopAtAnEdit(); err != nil {
		t.Fatal(err)
	}
	tr.writeRawHead(bogusObjectID(t, tr.repo.ObjectFormat).String() + "\n")

	if _, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime}); err == nil {
		t.Fatal("a rebase continued over a missing HEAD commit")
	}
}

func TestTheLastRebaseActionOfAFreshStateIsEmpty(t *testing.T) {
	if got := lastRebaseAction(RebaseState{}); got != "" {
		t.Fatalf("lastRebaseAction = %q", got)
	}
}
