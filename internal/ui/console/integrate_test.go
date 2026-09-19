package console

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

func forkedRepo(t *testing.T, conflict bool) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"shared.txt": "base\n"})
	r.run("branch feature " + base.String())
	if conflict {
		r.commit("ours", map[string]string{"shared.txt": "ours\n"})
	} else {
		r.commit("ours", map[string]string{"ours.txt": "ours\n"})
	}
	r.run("checkout feature")
	if conflict {
		r.commit("theirs", map[string]string{"shared.txt": "theirs\n"})
	} else {
		r.commit("theirs", map[string]string{"theirs.txt": "theirs\n"})
	}
	r.run("checkout main")
	return r
}

func TestMergeFastForwardsWhenItCan(t *testing.T) {
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("checkout -b feature " + base.String())
	r.commit("ahead", map[string]string{"b.txt": "b\n"})
	r.run("checkout main")

	got := r.run("merge feature")

	if !strings.HasPrefix(got, "Fast-forward ") {
		t.Fatalf("out = %q", got)
	}
}

func TestMergeSaysWhenThereIsNothingToDo(t *testing.T) {
	r := newTestRepo(t)
	r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("branch feature")

	if got := r.run("merge feature"); got != "Already up to date." {
		t.Fatalf("out = %q", got)
	}
}

func TestMergeMakesACommitWhenTheBranchesDiverged(t *testing.T) {
	r := forkedRepo(t, false)

	got := r.run("merge feature")

	if !strings.HasPrefix(got, "Merge made by the 'ort' strategy: ") {
		t.Fatalf("out = %q", got)
	}
}

func TestMergeNoFastForwardAlwaysCommits(t *testing.T) {
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("checkout -b feature " + base.String())
	r.commit("ahead", map[string]string{"b.txt": "b\n"})
	r.run("checkout main")

	got := r.run("merge --no-ff feature")

	if !strings.HasPrefix(got, "Merge made by the 'ort' strategy: ") {
		t.Fatalf("out = %q", got)
	}
}

func TestMergeFastForwardOnlyRefusesADivergedBranch(t *testing.T) {
	r := forkedRepo(t, false)

	if err := r.runFails("merge --ff-only feature"); err == nil {
		t.Fatal("a diverged branch must not fast-forward")
	}
}

func TestMergeSquashLeavesTheChangesStaged(t *testing.T) {
	r := forkedRepo(t, false)

	r.run("merge --squash feature")

	if got := r.run("status --porcelain"); got != "A  theirs.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestMergeStopsBeforeCommittingOnRequest(t *testing.T) {
	r := forkedRepo(t, false)

	if got := r.run("merge --no-commit feature"); got != "" {
		t.Fatalf("out = %q", got)
	}
}

func TestMergeTakesAMessage(t *testing.T) {
	r := forkedRepo(t, false)

	r.run(`merge --no-ff -m "joined together" feature`)

	if got := r.run("log --oneline -1"); !strings.HasSuffix(got, " joined together") {
		t.Fatalf("log = %q", got)
	}
}

func TestMergeReportsConflicts(t *testing.T) {
	r := forkedRepo(t, true)

	got := lines(r.run("merge feature"))

	if !slices.Contains(got, "CONFLICT: shared.txt") {
		t.Fatalf("out = %#v", got)
	}
}

func TestMergeRefusesBadArguments(t *testing.T) {
	r := forkedRepo(t, false)

	for _, line := range []string{"merge", "merge a b", "merge --no-ff --ff-only feature"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestRebaseReplaysTheBranch(t *testing.T) {
	r := forkedRepo(t, false)
	r.run("checkout feature")

	got := r.run("rebase main")

	if got != "Applied 1 commit(s)" {
		t.Fatalf("out = %q", got)
	}
}

func TestRebaseSaysWhenThereIsNothingToDo(t *testing.T) {
	r := newTestRepo(t)
	r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("branch feature")
	r.run("checkout feature")

	if got := r.run("rebase main"); got != "Current branch is up to date." {
		t.Fatalf("out = %q", got)
	}
}

func TestRebaseOntoAnotherBase(t *testing.T) {
	r := forkedRepo(t, false)
	r.run("checkout feature")

	r.run("rebase --onto main main")

	if got := lines(r.run("log --oneline")); len(got) != 3 {
		t.Fatalf("log = %#v", got)
	}
}

func TestRebaseStopsAtAConflictThenAborts(t *testing.T) {
	r := forkedRepo(t, true)
	r.run("checkout feature")

	got := lines(r.run("rebase main"))

	if len(got) == 0 || !strings.HasPrefix(got[0], "Stopped at ") {
		t.Fatalf("out = %#v", got)
	}
	if !slices.Contains(got, "CONFLICT: shared.txt") {
		t.Fatalf("out = %#v", got)
	}
	if abort := r.run("rebase --abort"); abort != "Rebase aborted" {
		t.Fatalf("out = %q", abort)
	}
}

func TestRebaseSkipsTheStoppedCommit(t *testing.T) {
	r := forkedRepo(t, true)
	r.run("checkout feature")
	r.run("rebase main")

	if got := r.run("rebase --skip"); got != "Applied 0 commit(s)" {
		t.Fatalf("out = %q", got)
	}
}

func TestRebaseContinuesAfterAResolution(t *testing.T) {
	r := forkedRepo(t, true)
	r.run("checkout feature")
	r.run("rebase main")
	r.write("shared.txt", "resolved\n")
	r.run("add shared.txt")

	if got := r.run("rebase --continue"); got != "Applied 1 commit(s)" {
		t.Fatalf("out = %q", got)
	}
}

func TestRebaseRefusesBadArguments(t *testing.T) {
	r := forkedRepo(t, false)

	for _, line := range []string{"rebase", "rebase a b"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestRebaseWithoutOneInProgressFails(t *testing.T) {
	r := forkedRepo(t, false)

	for _, line := range []string{"rebase --continue", "rebase --skip", "rebase --abort"} {
		if err := r.runFails(line); err == nil {
			t.Fatalf("%q must fail", line)
		}
	}
}

func TestMergeAndRebaseModesAreRejectedTogether(t *testing.T) {
	if _, err := mergeMode(mustOptions(t, []string{"--squash", "--no-ff"}, mergeOptions)); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
	if mode, err := mergeMode(mustOptions(t, []string{"--squash"}, mergeOptions)); err != nil || mode != ops.MergeSquash {
		t.Fatalf("mode = %v, err = %v", mode, err)
	}
}
