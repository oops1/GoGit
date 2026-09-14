package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (r *testRepo) threeOnAnUpToDateTopic() []hash.ObjectID {
	r.t.Helper()
	base := r.commitFiles("base", map[string]string{"f": tenLines("f")})
	r.createBranch("topic", base)
	r.switchTo("topic")
	return []hash.ObjectID{
		r.commitFiles("topic a", map[string]string{"a": "a\n"}),
		r.commitFiles("topic b", map[string]string{"b": "b\n"}),
		r.commitFiles("topic c", map[string]string{"c": "c\n"}),
	}
}

func TestAnInteractiveRebaseOfAnUpToDateBranchStillDropsCommits(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnAnUpToDateTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime, Todo: steps(actionPick, commits[0], actionDrop, commits[1], actionPick, commits[2])})

	if err != nil || result.UpToDate || !result.Finished() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.subjects(3); !slices.Equal(got, []string{"topic c", "topic a", "base"}) {
		t.Fatalf("history = %v", got)
	}
	if tr.linearHistory(tr.branchTarget("topic"), 2)[1].Message != "topic a\n" || tr.linearHistory(tr.branchTarget("topic"), 2)[0].Parents[0] != commits[0] {
		t.Fatal("the leading pick was not fast-forwarded")
	}
}

func TestAnInteractiveRebaseWithThePlannedPicksIsUpToDate(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnAnUpToDateTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime, Todo: steps(actionPick, commits[0], actionPick, commits[1], actionPick, commits[2])})

	if err != nil || !result.UpToDate || tr.branchTarget("topic") != commits[2] {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestAnInteractiveRebaseWithReorderedPicksIsNotUpToDate(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnAnUpToDateTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime, Todo: steps(actionPick, commits[0], actionPick, commits[2], actionPick, commits[1])})

	if err != nil || result.UpToDate {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.subjects(4); !slices.Equal(got, []string{"topic b", "topic c", "topic a", "base"}) {
		t.Fatalf("history = %v", got)
	}
}
