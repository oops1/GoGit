package ops

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func (r *testRepo) rebaseFork(conflict bool) (hash.ObjectID, hash.ObjectID) {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
	r.createBranch("topic", base)
	r.switchTo("topic")
	r.commitFiles("topic a", map[string]string{"a": "a\n"})
	line := 9
	if conflict {
		line = 4
	}
	r.commitWithAuthor("topic f\n\nbody", map[string]string{"f": changeLine(f, line, "TOPIC")})
	topic := r.commitFiles("topic b", map[string]string{"b": "b\n"})
	r.switchTo("main")
	upstream := r.commitFiles("main f", map[string]string{"f": changeLine(f, 4, "MAIN")})
	r.switchTo("topic")
	return topic, upstream
}

func rebaseOptions() RebaseOptions { return RebaseOptions{When: mergeTime} }

func (r *testRepo) rebaseState() RebaseState {
	r.t.Helper()
	state, err := ReadRebaseState(r.repo)
	if err != nil {
		r.t.Fatalf("ReadRebaseState returned error %v", err)
	}
	return state
}

func (r *testRepo) linearHistory(tip hash.ObjectID, count int) []*object.Commit {
	r.t.Helper()
	var out []*object.Commit
	db := r.db()
	for range count {
		c, err := db.Commit(tip)
		if err != nil {
			r.t.Fatal(err)
		}
		out = append(out, c)
		if len(c.Parents) != 1 {
			break
		}
		tip = c.Parents[0]
	}
	return out
}

func TestRebaseReplaysTheBranchOntoTheUpstream(t *testing.T) {
	tr := newTestRepo(t)
	topic, upstream := tr.rebaseFork(false)

	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || !result.Finished() || result.Applied != 3 || result.Old != topic {
		t.Fatalf("result = %+v, %v", result, err)
	}
	history := tr.linearHistory(tr.branchTarget("topic"), 4)
	if history[0].Message != "topic b\n" || history[1].Author.Name != "Other" || history[3].Message != "main f\n" || history[2].Parents[0] != upstream {
		t.Fatalf("history = %+v", history)
	}
	if head, attached := tr.headSymbolicTarget(); !attached || head != refs.BranchName("topic") || tr.rebaseState().InProgress() {
		t.Fatalf("HEAD = %s, attached %v", head, attached)
	}
}

func TestRebaseOfABranchAlreadyOnTheUpstreamChangesNothing(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.createBranch("topic", base)
	tr.switchTo("topic")
	tip := tr.commitFiles("topic", map[string]string{"g": "g\n"})

	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || !result.UpToDate || tr.branchTarget("topic") != tip {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestRebaseStopsOnAConflictAndContinuesWithTheAuthor(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)

	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || result.Finished() || len(result.Conflicts) != 1 || result.Applied != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	state := tr.rebaseState()
	if state.Stopped != result.Stopped || len(state.Todo) != 1 || len(state.Done) != 2 || state.Author.Name != "Other" || state.Message != "topic f\n\nbody\n" {
		t.Fatalf("state = %+v", state)
	}
	if merge := tr.mergeState(); merge.Operation() != OperationRebase {
		t.Fatalf("merge state = %+v", merge)
	}
	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrUnmergedPaths) {
		t.Fatalf("continue over conflicts: err = %v", err)
	}
	tr.writeFile("f", "resolved\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	result, err = ContinueRebase(t.Context(), tr.repo, rebaseOptions())

	if err != nil || !result.Finished() || result.Applied != 2 {
		t.Fatalf("continue = %+v, %v", result, err)
	}
	history := tr.linearHistory(tr.branchTarget("topic"), 3)
	if history[1].Author.Name != "Other" || history[1].Message != "topic f\n\nbody\n" || tr.readFile("f") != "resolved\n" {
		t.Fatalf("history = %+v", history)
	}
}

func TestAResolutionThatChangesNothingDropsTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}
	if err := ResolveConflicts(t.Context(), tr.repo, []string{"f"}, TakeOurs); err != nil {
		t.Fatal(err)
	}

	result, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions())

	if err != nil || !result.Finished() || result.Applied != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if history := tr.linearHistory(tr.branchTarget("topic"), 3); history[1].Message != "topic a\n" {
		t.Fatalf("history = %+v", history)
	}
}

func TestSkipDropsTheStoppedCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}

	result, err := SkipRebase(t.Context(), tr.repo, rebaseOptions())

	if err != nil || !result.Finished() || !strings.Contains(tr.readFile("f"), "MAIN") || tr.index().HasConflicts() {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestAbortingARebaseReturnsToTheBranch(t *testing.T) {
	tr := newTestRepo(t)
	topic, _ := tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}

	if err := AbortOperation(t.Context(), tr.repo); err != nil {
		t.Fatal(err)
	}

	head, attached := tr.headSymbolicTarget()
	if !attached || head != refs.BranchName("topic") || tr.branchTarget("topic") != topic || !strings.Contains(tr.readFile("f"), "TOPIC") || tr.exists("b") != true {
		t.Fatalf("HEAD = %s, topic moved or files not restored", head)
	}
	if tr.rebaseState().InProgress() || tr.mergeState().InProgress() {
		t.Fatal("the rebase survived the abort")
	}
}

func TestRebaseOntoAnotherBase(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(false)
	other := tr.branchTarget("main")

	result, err := Rebase(t.Context(), tr.repo, "main~1", RebaseOptions{Onto: "main", When: mergeTime})

	if err != nil || result.Applied != 3 || tr.linearHistory(tr.branchTarget("topic"), 4)[3].Message != "main f\n" || other != tr.branchTarget("main") {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestRebaseRefusesWhatItCannotDo(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(false)
	cases := map[string]struct {
		prepare  func()
		upstream string
		opts     RebaseOptions
		want     error
	}{
		"unknown upstream": {func() {}, "missing", rebaseOptions(), ErrTargetNotFound},
		"unknown onto":     {func() {}, "main", RebaseOptions{Onto: "missing"}, ErrTargetNotFound},
		"dirty tree":       {func() { tr.writeFile("keep", "dirty\n") }, "main", rebaseOptions(), ErrWouldOverwrite},
		"detached":         {func() { tr.writeFile("keep", "keep\n"); tr.writeRawHead(tr.branchTarget("topic").String() + "\n") }, "main", rebaseOptions(), ErrDetachedHead},
		"unborn":           {func() { tr.writeRawHead("ref: refs/heads/fresh\n") }, "main", rebaseOptions(), ErrUnbornHead},
	}
	for _, name := range []string{"unknown upstream", "unknown onto", "dirty tree", "detached", "unborn"} {
		c := cases[name]
		c.prepare()
		if _, err := Rebase(t.Context(), tr.repo, c.upstream, c.opts); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestRebaseWaitsForAMergeInProgress(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := Rebase(t.Context(), tr.repo, "feature", rebaseOptions()); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("err = %v", err)
	}
}

func TestRebaseNeedsAnIdentityAndAWorkingTree(t *testing.T) {
	if _, err := Rebase(t.Context(), newTestRepoNoIdentity(t).repo, "main", rebaseOptions()); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("identity: err = %v", err)
	}
	if _, err := Rebase(t.Context(), newBareTestRepo(t).repo, "main", rebaseOptions()); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("bare: err = %v", err)
	}
}

func TestContinueAndSkipNeedARebase(t *testing.T) {
	tr := newTestRepo(t)
	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrNoRebaseInProgress) {
		t.Fatalf("continue: err = %v", err)
	}
	if _, err := SkipRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrNoRebaseInProgress) {
		t.Fatalf("skip: err = %v", err)
	}
}

func TestAStepTheRebaseCannotRunStopsIt(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}
	state := tr.rebaseState()
	state.Todo[0].Action = "exec"
	if err := writeRebaseState(tr.repo, state); err != nil {
		t.Fatal(err)
	}

	if _, err := SkipRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrRebaseStepUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheRebaseStateSurvivesATripThroughTheFiles(t *testing.T) {
	tr := newTestRepo(t)
	id := hash.SumSHA1("commit", []byte("x"))
	author := object.Signature{Name: "O'Brien!", Email: "ob@example.com", When: time.Unix(1700000000, 0).In(time.FixedZone("", -(3*60+30)*60))}
	want := RebaseState{
		HeadName: "refs/heads/topic", Onto: id, OrigHead: id,
		Done:    []RebaseStep{{Action: actionPick, Commit: id, Subject: "done step"}},
		Todo:    []RebaseStep{{Action: actionPick, Commit: id, Subject: "todo step"}},
		Stopped: id, Message: "message\n", Author: &author, Rewritten: id.String() + " " + id.String() + "\n",
	}
	if err := writeRebaseState(tr.repo, want); err != nil {
		t.Fatal(err)
	}

	got := tr.rebaseState()

	if got.HeadName != want.HeadName || got.Onto != id || got.Stopped != id || got.Message != want.Message || got.Rewritten != want.Rewritten ||
		got.Author.Name != author.Name || !got.Author.When.Equal(author.When) || got.Author.When.Format("-0700") != "-0330" ||
		len(got.Done) != 1 || got.Todo[0] != want.Todo[0] {
		t.Fatalf("state = %+v", got)
	}
}

func TestACorruptRebaseStateIsRefused(t *testing.T) {
	good := "GIT_AUTHOR_NAME='a'\nGIT_AUTHOR_EMAIL='b'\nGIT_AUTHOR_DATE='@1 +0000'\n"
	for name, files := range map[string]map[string]string{
		"todo line":       {rebaseTodo: "pick\n"},
		"todo commit":     {rebaseTodo: "pick nothex subject\n"},
		"onto":            {rebaseOnto: "garbage\n"},
		"script line":     {rebaseAuthorScript: "no equals sign\n"},
		"script quoting":  {rebaseAuthorScript: "GIT_AUTHOR_NAME=bare\n"},
		"script unclosed": {rebaseAuthorScript: "GIT_AUTHOR_NAME='open\n"},
		"date":            {rebaseAuthorScript: strings.Replace(good, "@1 +0000", "@x +0000", 1)},
		"zone digits":     {rebaseAuthorScript: strings.Replace(good, "@1 +0000", "@1 +00x0", 1)},
		"zone sign":       {rebaseAuthorScript: strings.Replace(good, "@1 +0000", "@1 *0000", 1)},
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRepo(t)
			if err := tr.repo.Root().MkdirAll(rebaseDir, 0o777); err != nil {
				t.Fatal(err)
			}
			files[rebaseHeadName] = "refs/heads/topic\n"
			for file, content := range files {
				if err := tr.repo.Root().WriteFile(rebasePath(file), []byte(content), 0o666); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ReadRebaseState(tr.repo); err == nil {
				t.Fatal("a corrupt state was accepted")
			}
		})
	}
}

func TestAuthorScriptsDecodeGitsEscapes(t *testing.T) {
	sig, err := parseAuthorScript("GIT_AUTHOR_NAME='O'\\''Brien'\\!\nGIT_AUTHOR_EMAIL='e'\nGIT_AUTHOR_DATE='@5 -0100'\n")
	if err != nil || sig.Name != "O'Brien!" || sig.When.Unix() != 5 {
		t.Fatalf("sig = %+v, %v", sig, err)
	}
	if trimMessage("\n\n") != "" || trimMessage("a\n\n\n") != "a\n" {
		t.Fatal("messages are not trimmed like git")
	}
}

func TestRebaseSkipsMergesAndIgnoresUntrackedFiles(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(false)
	tr.switchTo("topic")
	if _, err := tr.merge("main", MergeOptions{Mode: MergeNoFastForward}); err != nil {
		t.Fatal(err)
	}
	tr.switchTo("main")
	tr.commitFiles("main again", map[string]string{"later": "later\n"})
	tr.switchTo("topic")
	tr.writeFile("untracked.txt", "mine\n")

	result, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions())

	if err != nil || !result.Finished() || result.Applied != 3 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	for _, c := range tr.linearHistory(tr.branchTarget("topic"), 4) {
		if len(c.Parents) > 1 {
			t.Fatalf("a merge commit was replayed: %+v", c)
		}
	}
	if tr.readFile("untracked.txt") != "mine\n" {
		t.Fatal("the untracked file was lost")
	}
}

func TestContinueFailsOnAnUnreadableIndex(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}
	tr.corruptIndexFile()

	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); err == nil {
		t.Fatal("a rebase continued over an unreadable index")
	}
}

func TestRebaseStopsWhenTheHistoryCannotBeWalked(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(false)
	db := tr.db()
	tree, err := db.PutObject(&object.Tree{})
	if err != nil {
		t.Fatal(err)
	}
	sig := testSignature()
	broken, err := db.PutObject(&object.Commit{Tree: tree, Parents: []hash.ObjectID{bogusObjectID(t, tr.repo.ObjectFormat)}, Author: sig, Committer: sig, Message: "broken\n"})
	if err != nil {
		t.Fatal(err)
	}
	tr.createBranch("broken", broken)

	if _, err := Rebase(t.Context(), tr.repo, "broken", RebaseOptions{Onto: "main", When: mergeTime}); err == nil {
		t.Fatal("a rebase ran over a missing commit")
	}
}

func TestARebaseThatCannotWriteTheResolutionFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}
	tr.writeFile("f", "resolved\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	swapDBPut(t, func(*odb.DB, object.Type, []byte) (hash.ObjectID, error) { return hash.Zero, errInjected })

	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestSkippingWithAnUnreadableHeadFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}
	tr.writeRawHead("garbage\n")

	if _, err := SkipRebase(t.Context(), tr.repo, rebaseOptions()); err == nil {
		t.Fatal("a skip ran over an unreadable HEAD")
	}
}

func TestTheRebaseStateNeedsItsDirectory(t *testing.T) {
	tr := newTestRepo(t)
	swapRootMkdirAllFailForPath(t, rebaseDir)

	if err := writeRebaseState(tr.repo, RebaseState{HeadName: "refs/heads/topic"}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestClearingTheRebaseStateReportsAFailedRemoval(t *testing.T) {
	tr := newTestRepo(t)
	original := fsRootRemoveAll
	fsRootRemoveAll = func(*os.Root, string) error { return errInjected }
	t.Cleanup(func() { fsRootRemoveAll = original })

	if err := clearRebaseState(tr.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestAStateWithoutAnAuthorScriptHasNoAuthor(t *testing.T) {
	sig, err := parseAuthorScript("  \n")
	if sig != nil || err != nil {
		t.Fatalf("sig = %+v, %v", sig, err)
	}
}

func TestPullWithRebaseNeedsAnIdentity(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.appendConfig("[pull]\n\trebase = true\n")
	client.repo = client.reopen()
	client.writeFile("c.txt", "local\n")
	mustStage(t, client, "c.txt")
	client.commitAll("local")
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("remote")

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v", err)
	}
}
