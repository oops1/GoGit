package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

var bisectFixtureFiles = []string{"BISECT_LOG", "BISECT_START", "BISECT_EXPECTED_REV", "BISECT_TERMS", "BISECT_NAMES", "BISECT_ANCESTORS_OK"}

func (r *testRepo) startBisect(start string, bad hash.ObjectID) {
	r.t.Helper()
	for _, name := range bisectFixtureFiles {
		r.writeFile(".git/"+name, "\n")
	}
	r.writeFile(".git/BISECT_START", start+"\n")
	r.writeFile(".git/BISECT_EXPECTED_REV", bad.String()+"\n")
	r.writeFile(".git/refs/bisect/bad", bad.String()+"\n")
}

func (r *testRepo) detachAt(id hash.ObjectID) {
	r.t.Helper()
	if err := Switch(r.t.Context(), r.repo, id.String(), SwitchOptions{}); err != nil {
		r.t.Fatalf("Switch returned error %v", err)
	}
}

func bisectingRepo(t *testing.T) (*testRepo, hash.ObjectID, hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t)
	first := r.initialCommit()
	second := r.commitFiles("second", map[string]string{"b.txt": "b\n"})
	r.createBranch("topic", second)
	r.detachAt(first)
	r.startBisect("main", second)
	return r, first, second
}

func TestReadMergeStateReportsABisectAndTheBranchItStartedFrom(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()
	r.startBisect("refs/heads/main", r.initialCommit())

	state, err := ReadMergeState(r.repo)

	if err != nil || !state.Bisecting || state.BisectStart != "main" || state.InProgress() {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestReadMergeStateIgnoresABisectStartWithoutItsLog(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".git/BISECT_START", "main\n")

	state, err := ReadMergeState(r.repo)

	if err != nil || state.Bisecting || state.BisectStart != "" {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestResetBisectReturnsToTheBranchItStartedFrom(t *testing.T) {
	r, _, second := bisectingRepo(t)

	if err := ResetBisect(t.Context(), r.repo); err != nil {
		t.Fatalf("ResetBisect returned error %v", err)
	}

	if target, ok := r.headSymbolicTarget(); !ok || target != refs.BranchName("main") || r.headCommit(r.refs()) != second {
		t.Fatalf("HEAD = %s, %v", target, ok)
	}
	for _, name := range bisectFixtureFiles {
		if r.exists(".git/" + name) {
			t.Fatalf("%s is left behind", name)
		}
	}
	if _, err := r.refs().Lookup("refs/bisect/bad"); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("refs/bisect/bad = %v", err)
	}
	if state, err := ReadMergeState(r.repo); err != nil || state.Bisecting {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestResetBisectWithoutACheckoutLeavesHeadAlone(t *testing.T) {
	r, first, _ := bisectingRepo(t)
	r.writeFile(".git/BISECT_HEAD", first.String()+"\n")

	if err := ResetBisect(t.Context(), r.repo); err != nil {
		t.Fatalf("ResetBisect returned error %v", err)
	}

	if _, symbolic := r.headSymbolicTarget(); symbolic || r.headCommit(r.refs()) != first || r.exists(".git/BISECT_HEAD") {
		t.Fatalf("HEAD moved or BISECT_HEAD is left behind")
	}
}

func TestResetBisectRefusesWhenNoBisectIsInProgress(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()

	if err := ResetBisect(t.Context(), r.repo); !errors.Is(err, ErrNotBisecting) {
		t.Fatalf("ResetBisect = %v, want ErrNotBisecting", err)
	}
}

func TestResetBisectKeepsTheStateWhenItCannotReturn(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		setup func(r *testRepo)
		want  error
	}{
		{"cancelled", cancelled, func(*testRepo) {}, context.Canceled},
		{"original branch is gone", t.Context(), func(r *testRepo) { r.writeFile(".git/BISECT_START", "gone\n") }, ErrTargetNotFound},
		{"unreadable bisect ref", t.Context(), func(r *testRepo) { r.writeFile(".git/refs/bisect/good-x", "not a ref\n") }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := bisectingRepo(t)
			tc.setup(r)

			err := ResetBisect(tc.ctx, r.repo)

			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("ResetBisect = %v, want %v", err, tc.want)
			}
			if !r.exists(".git/BISECT_START") {
				t.Fatal("a failed reset removed BISECT_START")
			}
		})
	}
}

func TestABisectedBranchCannotBeDeletedForcedOrCheckedOutElsewhere(t *testing.T) {
	r, first, second := bisectingRepo(t)
	r.writeFile(".git/BISECT_START", "topic\n")

	if err := DeleteBranch(t.Context(), r.repo, "topic", true); !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("DeleteBranch = %v", err)
	}
	if err := CreateBranch(t.Context(), r.repo, "topic", first, CreateBranchOptions{Force: true}); !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("CreateBranch = %v", err)
	}
	if err := checkBranchIsFree(r.repo, refs.BranchName("topic")); !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("checkBranchIsFree = %v", err)
	}
	if err := RenameBranch(t.Context(), r.repo, "topic", "renamed", false); !errors.Is(err, ErrBranchBisected) {
		t.Fatalf("RenameBranch = %v", err)
	}
	if got := r.branchTarget("topic"); got != second {
		t.Fatalf("topic = %s, want %s", got, second)
	}
}

func TestABisectedBranchCanBeRenamedOnceHeadIsOnABranch(t *testing.T) {
	r, _, _ := bisectingRepo(t)
	r.writeFile(".git/BISECT_START", "topic\n")
	r.switchTo("main")

	if err := RenameBranch(t.Context(), r.repo, "topic", "renamed", false); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
}
