package console

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

func twoRevisions(t *testing.T) (*testRepo, string, string) {
	t.Helper()
	r := newTestRepo(t)
	first := r.commit("first", map[string]string{"a.txt": "one\n"})
	second := r.commit("second", map[string]string{"a.txt": "two\n", "b.txt": "b\n"})
	return r, first.String(), second.String()
}

func TestDiffBetweenTwoRevisionsPrintsAUnifiedPatch(t *testing.T) {
	r, first, second := twoRevisions(t)

	got := r.run("diff " + first + " " + second)

	if !strings.HasPrefix(got, "diff --git a/a.txt b/a.txt") {
		t.Fatalf("diff = %q", got)
	}
	if !strings.Contains(got, "\n-one\n") || !strings.Contains(got, "\n+two\n") {
		t.Fatalf("diff = %q", got)
	}
}

func TestDiffWithOneRevisionComparesItWithHead(t *testing.T) {
	r, first, _ := twoRevisions(t)

	if got := r.run("diff " + first); !strings.Contains(got, "a/a.txt") {
		t.Fatalf("diff = %q", got)
	}
}

func TestDiffNameOnlyListsThePaths(t *testing.T) {
	r, first, second := twoRevisions(t)

	got := lines(r.run("diff --name-only " + first + " " + second))

	slices.Sort(got)
	if !slices.Equal(got, []string{"a.txt", "b.txt"}) {
		t.Fatalf("paths = %#v", got)
	}
}

func TestDiffStatAndNumstatSummariseTheChange(t *testing.T) {
	r, first, second := twoRevisions(t)

	stat := r.run("diff --stat " + first + " " + second)
	if !strings.Contains(stat, "a.txt") {
		t.Fatalf("stat = %q", stat)
	}
	numstat := lines(r.run("diff --numstat " + first + " " + second))
	if len(numstat) != 2 || !strings.HasSuffix(numstat[0], "a.txt") {
		t.Fatalf("numstat = %#v", numstat)
	}
}

func TestDiffNeedsRevisions(t *testing.T) {
	r, _, _ := twoRevisions(t)

	for _, line := range []string{"diff", "diff a b c"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestDiffRefusesAnUnknownRevision(t *testing.T) {
	r, first, _ := twoRevisions(t)

	if err := r.runFails("diff " + first + " nowhere"); err == nil {
		t.Fatal("an unknown revision must fail")
	}
}

func TestChangedPathsFallBackToTheOldName(t *testing.T) {
	got := changedPaths([]diff.File{{OldPath: "gone.txt"}, {NewPath: "fresh.txt"}})
	if !slices.Equal(got, []string{"gone.txt", "fresh.txt"}) {
		t.Fatalf("paths = %#v", got)
	}
}
