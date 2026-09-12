package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (r *testRepo) threeOnTopic() []hash.ObjectID {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
	r.createBranch("topic", base)
	r.switchTo("topic")
	first := r.commitFiles("topic a", map[string]string{"a": "a\n"})
	second := r.commitFiles("topic b", map[string]string{"b": "b\n"})
	third := r.commitFiles("topic c", map[string]string{"c": "c\n"})
	r.switchTo("main")
	r.commitFiles("main f", map[string]string{"f": changeLine(f, 0, "MAIN")})
	r.switchTo("topic")
	return []hash.ObjectID{first, second, third}
}

func steps(pairs ...any) []RebaseStep {
	todo := make([]RebaseStep, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		action, _ := pairs[i].(string)
		commit, _ := pairs[i+1].(hash.ObjectID)
		todo = append(todo, RebaseStep{Action: action, Commit: commit, Subject: "subject"})
	}
	return todo
}

func (r *testRepo) subjects(count int) []string {
	r.t.Helper()
	var out []string
	for _, commit := range r.linearHistory(r.branchTarget("topic"), count) {
		out = append(out, firstLine(commit.Message))
	}
	return out
}

func TestAnInteractiveRebaseDropsTheCommitsItIsTold(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionPick, commits[0], actionDrop, commits[1], actionPick, commits[2]),
	})

	if err != nil || !result.Finished() || result.Applied != 2 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.subjects(3); !slices.Equal(got, []string{"topic c", "topic a", "main f"}) {
		t.Fatalf("history = %v", got)
	}
	if tr.exists("b") {
		t.Fatal("the dropped commit left its file behind")
	}
}

func TestAnInteractiveRebaseSquashesAndFixesUp(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionPick, commits[0], actionSquash, commits[1], actionFixup, commits[2]),
	})

	if err != nil || !result.Finished() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	history := tr.linearHistory(tr.branchTarget("topic"), 2)
	if firstLine(history[1].Message) != "main f" {
		t.Fatalf("history = %+v", history)
	}
	if history[0].Message != "topic a\n\ntopic b\n" {
		t.Fatalf("message = %q", history[0].Message)
	}
	if !tr.exists("a") || !tr.exists("b") || !tr.exists("c") {
		t.Fatal("the folded commits lost their files")
	}
}

func TestAnInteractiveRebaseStopsForAReword(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionReword, commits[0], actionPick, commits[1]),
	})

	if err != nil || result.Finished() || !result.Amending() || result.Message != "topic a\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	state := tr.rebaseState()
	if !state.Amending() || state.Stopped != commits[0] {
		t.Fatalf("state = %+v", state)
	}

	done, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime, Message: "a better subject"})

	if err != nil || !done.Finished() {
		t.Fatalf("continue = %+v, %v", done, err)
	}
	if got := tr.subjects(3); !slices.Equal(got, []string{"topic b", "a better subject", "main f"}) {
		t.Fatalf("history = %v", got)
	}
}

func TestAnInteractiveRebaseStopsForAnEditAndKeepsTheMessage(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()

	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionEdit, commits[0], actionPick, commits[1]),
	}); err != nil {
		t.Fatalf("Rebase returned error %v", err)
	}
	tr.writeFile("a", "a changed while editing\n")
	if err := Stage(t.Context(), tr.repo, []string{"a"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	done, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime})

	if err != nil || !done.Finished() {
		t.Fatalf("continue = %+v, %v", done, err)
	}
	if got := tr.subjects(3); !slices.Equal(got, []string{"topic b", "topic a", "main f"}) {
		t.Fatalf("history = %v", got)
	}
	if tr.readFile("a") != "a changed while editing\n" {
		t.Fatalf("a = %q", tr.readFile("a"))
	}
}

func TestAnAmendingRebaseCanBeSkipped(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()
	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionEdit, commits[0], actionPick, commits[1]),
	}); err != nil {
		t.Fatal(err)
	}

	done, err := SkipRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime})

	if err != nil || !done.Finished() {
		t.Fatalf("skip = %+v, %v", done, err)
	}
	if got := tr.subjects(3); !slices.Equal(got, []string{"topic b", "topic a", "main f"}) {
		t.Fatalf("history = %v", got)
	}
}

func TestAnAmendingRebaseCanBeAborted(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()
	before := tr.branchTarget("topic")
	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionReword, commits[0], actionPick, commits[1]),
	}); err != nil {
		t.Fatal(err)
	}

	if err := AbortOperation(t.Context(), tr.repo); err != nil {
		t.Fatalf("AbortOperation returned error %v", err)
	}

	if tr.branchTarget("topic") != before {
		t.Fatal("the branch did not come back")
	}
	if tr.exists(".git/" + rebasePath(rebaseAmend)) {
		t.Fatal("the amend marker stayed behind")
	}
}

func TestAFoldedCommitKeepsTheAuthorOfTheFirstOne(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	tr.createBranch("topic", base)
	tr.switchTo("topic")
	first := tr.commitWithAuthor("topic a", map[string]string{"a": "a\n"})
	second := tr.commitFiles("topic b", map[string]string{"b": "b\n"})
	tr.switchTo("main")
	tr.commitFiles("main f", map[string]string{"f": changeLine(f, 0, "MAIN")})
	tr.switchTo("topic")

	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionPick, first, actionSquash, second),
	}); err != nil {
		t.Fatalf("Rebase returned error %v", err)
	}

	history := tr.linearHistory(tr.branchTarget("topic"), 1)
	if history[0].Author.Name != "Other" {
		t.Fatalf("author = %+v", history[0].Author)
	}
}

func TestAConflictDuringAFoldStopsTheRebase(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	tr.createBranch("topic", base)
	tr.switchTo("topic")
	first := tr.commitFiles("topic a", map[string]string{"a": "a\n"})
	second := tr.commitFiles("topic f", map[string]string{"f": changeLine(f, 4, "TOPIC")})
	tr.switchTo("main")
	tr.commitFiles("main f", map[string]string{"f": changeLine(f, 4, "MAIN")})
	tr.switchTo("topic")

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionPick, first, actionSquash, second),
	})

	if err != nil || result.Finished() || len(result.Conflicts) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if state := tr.rebaseState(); state.Amending() || state.Stopped != second {
		t.Fatalf("state = %+v", state)
	}
}

func TestATodoIsCheckedBeforeTheRebaseStarts(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()
	before := tr.branchTarget("topic")

	for _, todo := range [][]RebaseStep{
		steps("reverse", commits[0]),
		steps(actionSquash, commits[0]),
		{{Action: actionPick}},
		append(steps(actionDrop, commits[0]), RebaseStep{Action: actionFixup, Commit: commits[1]}),
	} {
		if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime, Todo: todo}); !errors.Is(err, ErrRebaseStepUnsupported) {
			t.Errorf("%+v: err = %v", todo, err)
		}
	}
	if tr.branchTarget("topic") != before {
		t.Fatal("a refused todo moved the branch")
	}
}

func TestAFoldedMessageKeepsTheFirstOneOnAFixup(t *testing.T) {
	if got := foldedMessage(actionFixup, "first\n", "second\n"); got != "first\n" {
		t.Fatalf("fixup = %q", got)
	}
	if got := foldedMessage(actionSquash, "first\n", "second\n"); got != "first\n\nsecond\n" {
		t.Fatalf("squash = %q", got)
	}
}

func TestTheRewrittenListFollowsAFold(t *testing.T) {
	first, second, folded := idOf("one"), idOf("two"), idOf("folded")
	rewritten := first.String() + " " + second.String() + "\n"

	got := rewrittenAfterFold(rewritten, second, first, folded)

	want := first.String() + " " + folded.String() + "\n" + first.String() + " " + folded.String() + "\n"
	if got != want {
		t.Fatalf("rewritten = %q, want %q", got, want)
	}
}

func idOf(text string) hash.ObjectID { return hash.SumSHA1(object.TypeBlob.String(), []byte(text)) }

func TestARewordOfACommitThatIsAlreadyThereAppliesNothing(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	tr.createBranch("topic", base)
	tr.switchTo("topic")
	shared := tr.commitFiles("shared change", map[string]string{"f": changeLine(f, 4, "SHARED")})
	tr.switchTo("main")
	tr.commitFiles("shared change", map[string]string{"f": changeLine(f, 4, "SHARED")})
	tr.switchTo("topic")

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionReword, shared),
	})

	if err != nil || !result.Finished() || result.Applied != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestTheRewrittenListOfAFirstFoldStartsEmpty(t *testing.T) {
	original, commit := idOf("one"), idOf("folded")

	got := rewrittenAfterFold("", hash.Zero, original, commit)

	if got != original.String()+" "+commit.String()+"\n" {
		t.Fatalf("rewritten = %q", got)
	}
}

func TestContinuingAnAmendmentNeedsAReadableIndex(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()
	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionEdit, commits[0], actionPick, commits[1]),
	}); err != nil {
		t.Fatal(err)
	}
	tr.corruptIndexFile()

	if _, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime}); err == nil {
		t.Fatal("a corrupt index was accepted")
	}
}

func TestContinuingAnAmendmentRefusesUnmergedPaths(t *testing.T) {
	tr := newTestRepo(t)
	commits := tr.threeOnTopic()
	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{
		When: mergeTime,
		Todo: steps(actionEdit, commits[0], actionPick, commits[1]),
	}); err != nil {
		t.Fatal(err)
	}
	idx := tr.index()
	entry, ok := idx.Get("a", index.StageMerged)
	if !ok {
		t.Fatal("a is not in the index")
	}
	idx.Remove("a")
	entry.Stage = index.StageOurs
	idx.Add(*entry)
	tr.saveIndex(idx)

	if _, err := ContinueRebase(t.Context(), tr.repo, RebaseOptions{When: mergeTime}); !errors.Is(err, ErrUnmergedPaths) {
		t.Fatalf("err = %v", err)
	}
}
