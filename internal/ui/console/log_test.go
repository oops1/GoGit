package console

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/revision"
)

func threeCommits(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.commit("first", map[string]string{"a.txt": "a\n"})
	r.commit("second", map[string]string{"b.txt": "b\n"})
	r.commit("third", map[string]string{"c.txt": "c\n"})
	return r
}

func TestLogOnelinePrintsShortHashesAndSubjects(t *testing.T) {
	r := threeCommits(t)

	got := lines(r.run("log --oneline"))

	if len(got) != 3 {
		t.Fatalf("log = %#v", got)
	}
	for i, want := range []string{"third", "second", "first"} {
		if !strings.HasSuffix(got[i], " "+want) || len(got[i]) != shortHashLength+1+len(want) {
			t.Fatalf("line %d = %q, want a short hash and %q", i, got[i], want)
		}
	}
}

func TestLogCountsCommitsWithBothSpellings(t *testing.T) {
	r := threeCommits(t)

	for _, line := range []string{"log --oneline -2", "log --oneline -n2", "log --oneline -n 2", "log --oneline --max-count=2"} {
		if got := lines(r.run(line)); len(got) != 2 {
			t.Fatalf("%q gave %#v", line, got)
		}
	}
}

func TestLogSkipsCommits(t *testing.T) {
	r := threeCommits(t)

	got := lines(r.run("log --oneline --skip=2"))

	if len(got) != 1 || !strings.HasSuffix(got[0], " first") {
		t.Fatalf("log = %#v", got)
	}
}

func TestLogWalksBackwardsOnRequest(t *testing.T) {
	r := threeCommits(t)

	got := lines(r.run("log --oneline --reverse"))

	if len(got) != 3 || !strings.HasSuffix(got[0], " first") {
		t.Fatalf("log = %#v", got)
	}
}

func TestLogTakesRevisionArguments(t *testing.T) {
	r := threeCommits(t)

	got := lines(r.run("log --oneline HEAD~2..HEAD"))

	if len(got) != 2 {
		t.Fatalf("log = %#v", got)
	}
}

func TestLogFollowsTheFirstParentOnly(t *testing.T) {
	r := threeCommits(t)

	if got := lines(r.run("log --oneline --first-parent")); len(got) != 3 {
		t.Fatalf("log = %#v", got)
	}
}

func TestTheLongLogLooksLikeGit(t *testing.T) {
	r := newTestRepo(t)
	id := r.commit("subject\n\nbody line\n", map[string]string{"a.txt": "a\n"})

	got := lines(r.run("log"))

	want := []string{
		"commit " + id.String(),
		"Author: Go Git <gogit@example.com>",
		"Date:   " + r.env.Now.Format(gitDateLayout),
		"",
		"    subject",
		messageIndent,
		"    body line",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("log = %#v, want %#v", got, want)
	}
}

func TestTheLongLogNamesTheParentsOfAMerge(t *testing.T) {
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("branch feature " + base.String())
	r.commit("ours", map[string]string{"b.txt": "b\n"})
	r.run("checkout feature")
	r.commit("theirs", map[string]string{"c.txt": "c\n"})
	r.run("checkout main")
	r.run("merge --no-ff feature")

	got := r.run("log -1")

	if !strings.Contains(got, "\nMerge: ") {
		t.Fatalf("log = %q", got)
	}
}

func TestLogRefusesABadCount(t *testing.T) {
	r := threeCommits(t)
	for _, line := range []string{"log --max-count=x", "log --skip=-1"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestLogRefusesAnUnknownRevision(t *testing.T) {
	r := threeCommits(t)
	if err := r.runFails("log no-such-branch"); !errors.Is(err, revision.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLogOfAnEmptyRepositoryFails(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("log"); err == nil {
		t.Fatal("log without commits must fail")
	}
}

func TestSubjectOfSkipsLeadingBlankLines(t *testing.T) {
	if got := subjectOf("\n\n  hello \nrest\n"); got != "hello" {
		t.Fatalf("subject = %q", got)
	}
	if got := subjectOf(""); got != "" {
		t.Fatalf("subject = %q", got)
	}
}

func TestSplitShortCountKeepsEverythingElse(t *testing.T) {
	kept, count := splitShortCount([]string{"--oneline", "-5", "HEAD"})
	if count != "5" || !slices.Equal(kept, []string{"--oneline", "HEAD"}) {
		t.Fatalf("kept = %#v, count = %q", kept, count)
	}
}
