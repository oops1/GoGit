package console

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

func TestAddStagesTheNamedPaths(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", "a\n")
	r.write("b.txt", "b\n")

	if got := r.run("add a.txt b.txt"); got != "Staged 2 pathspec(s)" {
		t.Fatalf("out = %q", got)
	}
	got := lines(r.run("status --porcelain"))
	slices.Sort(got)
	if !slices.Equal(got, []string{"A  a.txt", "A  b.txt"}) {
		t.Fatalf("status = %#v", got)
	}
}

func TestAddAllStagesEverything(t *testing.T) {
	r := newTestRepo(t)
	r.write("nested/a.txt", "a\n")

	r.run("add -A")

	if got := r.run("status --porcelain"); got != "A  nested/a.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestAddUpdateStagesOnlyTrackedChanges(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"tracked.txt": "one\n"})
	r.write("tracked.txt", "two\n")
	r.write("fresh.txt", "fresh\n")

	r.run("add -u")

	got := lines(r.run("status --porcelain"))
	slices.Sort(got)
	if !slices.Equal(got, []string{"?? fresh.txt", "M  tracked.txt"}) {
		t.Fatalf("status = %#v", got)
	}
}

func TestAddUpdateWithNothingToStageFails(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("add -u"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddWithoutPathsFails(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("add"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddStagesAnIgnoredPathOnlyWithForce(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{".gitignore": "hidden.txt\n"})
	r.write("hidden.txt", "hidden\n")

	r.run("add hidden.txt")
	if got := r.run("status --porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
	r.run("add -f hidden.txt")
	if got := r.run("status --porcelain"); got != "A  hidden.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestAddRefusesAPathOutsideTheRepository(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("add ../outside.txt"); !errors.Is(err, ops.ErrInvalidPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommitRecordsTheStagedChanges(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", "a\n")
	r.run("add a.txt")

	out := r.run(`commit -m "the subject"`)

	if !strings.HasSuffix(out, "] the subject") || len(out) != shortHashLength+len("[] the subject") {
		t.Fatalf("out = %q", out)
	}
	if got := r.run("log --oneline"); !strings.HasSuffix(got, " the subject") {
		t.Fatalf("log = %q", got)
	}
}

func TestCommitAllStagesTrackedChangesFirst(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "one\n"})
	r.write("a.txt", "two\n")

	r.run("commit -a -m second")

	if got := r.run("status --porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
}

func TestCommitAllWithNothingToStageStillFails(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "one\n"})

	if err := r.runFails("commit -a -m nothing"); !errors.Is(err, ops.ErrNothingToCommit) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommitAmendRewritesTheLastCommit(t *testing.T) {
	r := newTestRepo(t)
	r.commit("first", map[string]string{"a.txt": "a\n"})

	r.run("commit --amend -m reworded")

	got := lines(r.run("log --oneline"))
	if len(got) != 1 || !strings.HasSuffix(got[0], " reworded") {
		t.Fatalf("log = %#v", got)
	}
}

func TestCommitAllowsAnEmptyCommitOnRequest(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("commit -m empty"); !errors.Is(err, ops.ErrNothingToCommit) {
		t.Fatalf("err = %v", err)
	}
	r.run("commit --allow-empty -m empty")
}

func TestCommitNeedsAMessageOrAnAmend(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("commit"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommitRefusesPositionalArguments(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("commit -m hi a.txt"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommitSkipsTheHooksOnRequest(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", "a\n")
	r.run("add a.txt")

	r.run("commit --no-verify -m hooked")
}

func TestResetMovesHeadInEveryMode(t *testing.T) {
	for _, mode := range []string{"--soft", "--mixed", "--hard", ""} {
		t.Run("mode"+mode, func(t *testing.T) {
			r := newTestRepo(t)
			first := r.commit("first", map[string]string{"a.txt": "a\n"})
			r.commit("second", map[string]string{"b.txt": "b\n"})

			out := r.run(strings.TrimSpace("reset " + mode + " " + first.String()))

			if out != "HEAD is now at "+first.String()[:shortHashLength] {
				t.Fatalf("out = %q", out)
			}
			if got := lines(r.run("log --oneline")); len(got) != 1 {
				t.Fatalf("log = %#v", got)
			}
		})
	}
}

func TestResetWithoutATargetStaysOnHead(t *testing.T) {
	r := newTestRepo(t)
	head := r.commit("initial", map[string]string{"a.txt": "a\n"})

	if got := r.run("reset"); got != "HEAD is now at "+head.String()[:shortHashLength] {
		t.Fatalf("out = %q", got)
	}
}

func TestResetUnstagesTheNamedPaths(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.write("a.txt", "changed\n")
	r.run("add a.txt")

	r.run("reset HEAD -- a.txt")

	if got := r.run("status --porcelain"); got != " M a.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestResetUnstagesPathsWithoutTheSeparator(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.write("a.txt", "changed\n")
	r.run("add a.txt")

	r.run("reset HEAD a.txt")

	if got := r.run("status --porcelain"); got != " M a.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestResetRefusesTwoModes(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("reset --soft --hard"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestResetOfPathsRefusesAMode(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("reset --hard HEAD -- a.txt"); err == nil {
		t.Fatal("resetting paths with a mode must fail")
	}
}

func TestResetOnlyPathsAfterTheSeparatorUsesHead(t *testing.T) {
	target, paths := resetTargetAndPaths(mustOptions(t, []string{"--", "a.txt"}, resetOptions))
	if target != "HEAD" || !slices.Equal(paths, []string{"a.txt"}) {
		t.Fatalf("target = %q, paths = %#v", target, paths)
	}
}

func mustOptions(t *testing.T, args []string, specs []option) options {
	t.Helper()
	opts, err := parseOptions(args, specs)
	if err != nil {
		t.Fatal(err)
	}
	return opts
}
