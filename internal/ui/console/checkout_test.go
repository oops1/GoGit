package console

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

func TestCheckoutAndSwitchMoveHead(t *testing.T) {
	for _, command := range []string{"checkout", "switch"} {
		t.Run(command, func(t *testing.T) {
			r := newTestRepo(t)
			r.commit("initial", map[string]string{"a.txt": "a\n"})
			r.run("branch feature")

			if got := r.run(command + " feature"); got != "Switched to 'feature'" {
				t.Fatalf("out = %q", got)
			}
			if got := r.run("branch"); !slices.Contains(lines(got), "* feature") {
				t.Fatalf("branch = %q", got)
			}
		})
	}
}

func TestCheckoutCreatesABranchAndSwitchesToIt(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("first", map[string]string{"a.txt": "a\n"})
	r.commit("second", map[string]string{"b.txt": "b\n"})

	if got := r.run("checkout -b work"); got != "Switched to a new branch 'work'" {
		t.Fatalf("out = %q", got)
	}
	r.run("checkout -b from-first " + first.String())

	if got := r.run("branch"); !slices.Contains(lines(got), "* from-first") {
		t.Fatalf("branch = %q", got)
	}
}

func TestSwitchCreatesABranchWithItsOwnFlag(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	r.run("switch -c work")

	if got := r.run("branch"); !slices.Contains(lines(got), "* work") {
		t.Fatalf("branch = %q", got)
	}
}

func TestCheckoutWantsExactlyOneTarget(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	for _, line := range []string{"checkout", "checkout a b", "checkout -b work a b"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestCheckoutRefusesAnUnknownTarget(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("checkout nowhere"); err == nil {
		t.Fatal("checking out a missing branch must fail")
	}
}

func TestCheckoutForcesOverLocalChanges(t *testing.T) {
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"a.txt": "base\n"})
	r.run("checkout -b feature " + base.String())
	r.commit("feature", map[string]string{"a.txt": "feature\n"})
	r.run("checkout main")
	r.write("a.txt", "local\n")

	if err := r.runFails("checkout feature"); err == nil {
		t.Fatal("switching over local changes must fail")
	}
	r.run("checkout -f feature")
}

func TestSwitchTracksTheRemoteBranchOnRequest(t *testing.T) {
	r := newTestRepo(t)
	id := r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.setRemoteBranch("origin", "shared", id)
	r.run("remote add origin " + r.dir)
	r.reopen()

	r.run("switch -c shared -t origin/shared")
	r.reopen()

	if got := r.run("config branch.shared.remote"); got != "origin" {
		t.Fatalf("upstream = %q", got)
	}
}

func TestStartBranchRefusesAnInvalidName(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("checkout -b ..bad"); !errors.Is(err, ops.ErrInvalidBranchName) {
		t.Fatalf("err = %v", err)
	}
}
