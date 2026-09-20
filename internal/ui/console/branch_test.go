package console

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func TestBranchListsTheLocalBranchesAndMarksTheCurrentOne(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("branch feature")

	got := lines(r.run("branch"))

	if !slices.Equal(got, []string{"  feature", "* main"}) {
		t.Fatalf("branch = %#v", got)
	}
}

func TestBranchVerboseAddsTheTipAndItsSubject(t *testing.T) {
	r := newTestRepo(t)
	id := r.commit("the subject", map[string]string{"a.txt": "a\n"})

	got := r.run("branch -v")

	if got != "* main "+id.String()[:shortHashLength]+" the subject" {
		t.Fatalf("branch = %q", got)
	}
}

func TestBranchCreatesAtHeadAndAtAGivenStartPoint(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("first", map[string]string{"a.txt": "a\n"})
	r.commit("second", map[string]string{"b.txt": "b\n"})

	if got := r.run("branch from-head"); !strings.HasPrefix(got, "Created branch from-head at ") {
		t.Fatalf("out = %q", got)
	}
	r.run("branch from-first " + first.String())

	if got := lines(r.run("branch")); !slices.Contains(got, "  from-first") {
		t.Fatalf("branch = %#v", got)
	}
}

func TestBranchForcesAnExistingNameOnlyWithTheFlag(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("first", map[string]string{"a.txt": "a\n"})
	second := r.commit("second", map[string]string{"b.txt": "b\n"})
	r.run("branch feature " + first.String())

	if err := r.runFails("branch feature " + second.String()); !errors.Is(err, ops.ErrBranchExists) {
		t.Fatalf("err = %v", err)
	}
	r.run("branch -f feature " + second.String())
}

func TestBranchDeletesBranches(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("branch one")
	r.run("branch two")

	got := lines(r.run("branch -d one two"))

	if !slices.Equal(got, []string{"Deleted branch one", "Deleted branch two"}) {
		t.Fatalf("out = %#v", got)
	}
	if got := r.run("branch"); got != "* main" {
		t.Fatalf("branch = %q", got)
	}
}

func TestBranchDeleteNeedsANameAndReportsFailures(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("branch -d"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
	if err := r.runFails("branch -d missing"); !errors.Is(err, ops.ErrBranchNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestBranchForceDeleteDropsAnUnmergedBranch(t *testing.T) {
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("checkout -b feature " + base.String())
	r.commit("only here", map[string]string{"b.txt": "b\n"})
	r.run("checkout main")

	if err := r.runFails("branch -d feature"); !errors.Is(err, ops.ErrBranchNotMerged) {
		t.Fatalf("err = %v", err)
	}
	r.run("branch -D feature")
}

func TestBranchRenamesTheCurrentBranchAndAnyOther(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("branch other")

	if got := r.run("branch -m trunk"); got != "Renamed branch main to trunk" {
		t.Fatalf("out = %q", got)
	}
	r.run("branch -m other second")

	got := lines(r.run("branch"))
	if !slices.Equal(got, []string{"  second", "* trunk"}) {
		t.Fatalf("branch = %#v", got)
	}
}

func TestBranchRenameRefusesTheWrongNumberOfNames(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("branch -m a b c"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestBranchRenameOfADetachedHeadFails(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("checkout " + first.String())

	if err := r.runFails("branch -m trunk"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestBranchForceRenameOverwritesTheTarget(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("branch one")
	r.run("branch two")

	if err := r.runFails("branch -m one two"); !errors.Is(err, ops.ErrBranchExists) {
		t.Fatalf("err = %v", err)
	}
	r.run("branch -M one two")
}

func TestBranchCreateRefusesExtraNames(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("branch a b c"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestBranchCreateRefusesAnUnknownStartPoint(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("branch feature nowhere"); !errors.Is(err, revision.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestBranchShowsRemoteBranches(t *testing.T) {
	r := newTestRepo(t)
	id := r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.setRemoteBranch("origin", "main", id)

	remotesOnly := lines(r.run("branch -r"))
	if !slices.Equal(remotesOnly, []string{"  remotes/origin/main"}) {
		t.Fatalf("branch -r = %#v", remotesOnly)
	}
	all := lines(r.run("branch -a"))
	if !slices.Equal(all, []string{"* main", "  remotes/origin/main"}) {
		t.Fatalf("branch -a = %#v", all)
	}
	verbose := lines(r.run("branch -a -v"))
	if len(verbose) != 2 || !strings.Contains(verbose[1], id.String()[:shortHashLength]) {
		t.Fatalf("branch -a -v = %#v", verbose)
	}
}

func TestBranchListingOfAnEmptyRepositoryIsEmpty(t *testing.T) {
	r := newTestRepo(t)
	if got := r.run("branch"); got != "" {
		t.Fatalf("branch = %q", got)
	}
}
