package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (r *testRepo) emptyAndRedundantOnTopic() []hash.ObjectID {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f})
	r.createBranch("topic", base)
	r.commitFiles("main f and g", map[string]string{"f": changeLine(f, 1, "MAIN"), "g": "g\n"})
	r.switchTo("topic")
	return []hash.ObjectID{
		r.commitAll("started empty"),
		r.commitFiles("only g", map[string]string{"g": "g\n"}),
		r.commitFiles("topic h", map[string]string{"h": "h\n"}),
	}
}

func TestARebaseKeepsACommitThatStartedEmptyAndDropsOneThatBecameEmpty(t *testing.T) {
	tr := newTestRepo(t)
	tr.emptyAndRedundantOnTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || !result.Finished() || result.Applied != 2 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.subjects(4); !slices.Equal(got, []string{"topic h", "started empty", "main f and g", "base"}) {
		t.Fatalf("history = %v", got)
	}
}

func TestAnEditOfACommitThatBecameEmptyStillStops(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.emptyAndRedundantOnTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime, Todo: steps(actionPick, commits[0], actionEdit, commits[1], actionPick, commits[2])})

	if err != nil || result.Finished() || !result.Amending() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	head, err := resolveHeadCommit(tr.refs())
	if err != nil || result.Amend != head {
		t.Fatalf("amend = %s, HEAD = %s, %v", result.Amend, head, err)
	}
	done, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime, Message: result.Message})
	if err != nil || !done.Finished() {
		t.Fatalf("continue = %+v, %v", done, err)
	}
	if got := tr.subjects(4); !slices.Equal(got, []string{"topic h", "started empty", "main f and g", "base"}) {
		t.Fatalf("history = %v", got)
	}
}
