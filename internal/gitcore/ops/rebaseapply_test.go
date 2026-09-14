package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func (r *testRepo) stopGitAm() (hash.ObjectID, hash.ObjectID) {
	r.t.Helper()
	before := r.commitFiles("base", map[string]string{"f": "base\n"})
	applied := r.commitFiles("applied by am", map[string]string{"g": "g\n"})
	r.writeFile(".git/"+origHeadFile, before.String()+"\n")
	r.writeFile(".git/"+rebaseApplyDir+"/applying", "")
	return before, applied
}

func TestAStoppedGitAmIsAnOperationInProgress(t *testing.T) {
	tr := newTestRepo(t)
	tr.stopGitAm()
	tr.createBranch("other", tr.branchTarget("main"))

	if state := tr.mergeState(); state.Operation() != OperationRebase {
		t.Fatalf("merge state = %+v", state)
	}
	if _, err := tr.merge("other", MergeOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("merge: err = %v", err)
	}
	if _, err := Rebase(t.Context(), tr.repo, "other", rebaseOptions()); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("rebase: err = %v", err)
	}
	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrRebaseApplyInProgress) {
		t.Fatalf("continue: err = %v", err)
	}
	if _, err := SkipRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrRebaseApplyInProgress) {
		t.Fatalf("skip: err = %v", err)
	}
}

func TestAbortingAStoppedGitAmReturnsTheBranchToOrigHead(t *testing.T) {
	tr := newTestRepo(t)
	before, _ := tr.stopGitAm()

	if err := AbortOperation(t.Context(), tr.repo); err != nil {
		t.Fatalf("AbortOperation returned error %v", err)
	}

	if got := tr.branchTarget("main"); got != before {
		t.Fatalf("main = %s, want %s", got, before)
	}
	if head, attached := tr.headSymbolicTarget(); !attached || head != refs.BranchName("main") {
		t.Fatalf("HEAD = %s, attached %v", head, attached)
	}
	if tr.exists("g") || tr.readFile("f") != "base\n" || tr.exists(".git/"+rebaseApplyDir) || tr.mergeState().InProgress() {
		t.Fatal("the abort left the applied state behind")
	}
}

func TestAbortingAStoppedRebaseApplyReturnsToTheBranch(t *testing.T) {
	tr := newTestRepo(t)
	topic, upstream := tr.rebaseFork(false)
	tr.switchTo("main")
	tr.writeRawHead(upstream.String() + "\n")
	tr.writeFile(".git/"+rebaseApplyDir+"/head-name", "refs/heads/topic\n")
	tr.writeFile(".git/"+rebaseApplyDir+"/"+rebaseOrigHead, topic.String()+"\n")

	if err := AbortOperation(t.Context(), tr.repo); err != nil {
		t.Fatalf("AbortOperation returned error %v", err)
	}

	if head, attached := tr.headSymbolicTarget(); !attached || head != refs.BranchName("topic") || tr.branchTarget("topic") != topic {
		t.Fatalf("HEAD = %s, attached %v", head, attached)
	}
	if !tr.exists("b") || tr.exists(".git/"+rebaseApplyDir) {
		t.Fatal("the abort did not restore the branch")
	}
}

func TestAbortingARebaseStartedOnADetachedHeadDetachesAtTheStart(t *testing.T) {
	tr := newTestRepo(t)
	topic, upstream := tr.rebaseFork(false)
	tr.writeRawHead(upstream.String() + "\n")
	if err := writeRebaseState(tr.repo, RebaseState{HeadName: "detached HEAD", Onto: upstream, OrigHead: topic}); err != nil {
		t.Fatal(err)
	}

	if err := AbortOperation(t.Context(), tr.repo); err != nil {
		t.Fatalf("AbortOperation returned error %v", err)
	}

	head, err := resolveHeadTarget(tr.refs())
	if err != nil || !head.detached || head.old != topic {
		t.Fatalf("HEAD = %+v, %v", head, err)
	}
}

func TestAbortingWithoutAnOriginalHeadIsRefused(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "base\n"})
	tr.writeFile(".git/"+rebaseApplyDir+"/applying", "")

	if err := AbortOperation(t.Context(), tr.repo); !errors.Is(err, ErrRebaseStateCorrupt) {
		t.Fatalf("err = %v", err)
	}
	if !tr.exists("f") {
		t.Fatal("the refused abort touched the working tree")
	}
}

func TestTheRebaseHeadIsReadFromEitherStateDirectory(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a corrupt orig-head":        {rebasePath(rebaseHeadName): "refs/heads/topic\n", rebasePath(rebaseOrigHead): "garbage\n"},
		"an unreadable apply head":   {rebaseApplyDir + "/head-name/blocked": ""},
		"a corrupt ORIG_HEAD for am": {rebaseApplyDir + "/applying": "", origHeadFile: "garbage\n"},
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRepo(t)
			for file, content := range files {
				tr.writeFile(".git/"+file, content)
			}
			if _, err := readRebaseHead(tr.repo); err == nil {
				t.Fatal("a corrupt rebase head was accepted")
			}
		})
	}
}

func TestATodoKeepsTheLinesItCannotRunAndRefusesToContinue(t *testing.T) {
	tr := newTestRepo(t)
	tr.rebaseFork(true)
	if _, err := Rebase(t.Context(), tr.repo, "main", rebaseOptions()); err != nil {
		t.Fatal(err)
	}
	extra := "exec make test\nbreak\nlabel onto\nfixup -C 1234567 subject\nnoop\n"
	todoFile := ".git/" + rebasePath(rebaseTodo)
	tr.writeFile(todoFile, tr.readFile(todoFile)+extra)
	before := tr.readFile(todoFile)

	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); !errors.Is(err, ErrRebaseStepUnsupported) {
		t.Fatalf("continue: err = %v", err)
	}
	if tr.readFile(todoFile) != before {
		t.Fatal("the refused continue rewrote the todo")
	}
	state := tr.rebaseState()
	if err := writeRebaseState(tr.repo, state); err != nil {
		t.Fatal(err)
	}
	if got := tr.readFile(todoFile); !strings.Contains(got, "exec make test\nbreak\nlabel onto\nfixup -C 1234567 subject\n") {
		t.Fatalf("todo = %q", got)
	}
	if err := AbortOperation(t.Context(), tr.repo); err != nil || tr.rebaseState().InProgress() {
		t.Fatalf("abort = %v", err)
	}
}

func TestATodoAcceptsGitsShortCommandNames(t *testing.T) {
	id := hash.SumSHA1("commit", []byte("x")).String()
	steps, err := parseTodo("p " + id + " one\nr " + id + "\ne " + id + "\ns " + id + "\nf " + id + "\nd " + id + "\n")
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, step := range steps {
		actions = append(actions, step.Action)
	}
	if got := strings.Join(actions, " "); got != "pick reword edit squash fixup drop" || steps[0].Subject != "one" {
		t.Fatalf("actions = %q, steps = %+v", got, steps)
	}
}
