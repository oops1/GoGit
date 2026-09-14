//go:build oracle

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func gitDirHas(dir, name string) string {
	if _, err := os.Stat(filepath.Join(dir, ".git", name)); err != nil {
		return name + " absent"
	}
	return name + " present"
}

func stoppedByGitState(b *mergeBuilder) string {
	b.o.t.Helper()
	branch, _ := b.o.attempt(b.dir, "symbolic-ref", "-q", "HEAD")
	return strings.Join([]string{
		b.o.run(b.dir, "rev-parse", "HEAD", "main", "topic"),
		branch,
		b.o.run(b.dir, "status", "--porcelain", "-uno"),
		b.o.run(b.dir, "ls-files", "-s"),
		stashStateFiles(b.o, b.dir),
		gitDirHas(b.dir, rebaseApplyDir) + ", " + gitDirHas(b.dir, rebaseDir),
	}, "\n== ")
}

func applyConflictHistory(b *mergeBuilder) {
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f})
	b.git("checkout", "-q", "-b", "topic")
	b.commit("topic f", map[string]string{"f": editLine(f, 3, "TOPIC")})
	b.commit("topic g", map[string]string{"g": "g\n"})
	b.o.write(b.dir, ".git/topic.mbox", b.o.run(b.dir, "format-patch", "--stdout", "main..topic"))
	b.git("checkout", "-q", "main")
	b.commit("main f", map[string]string{"f": editLine(f, 3, "MAIN")})
}

func TestOracleWeAbortWhatGitAmAndRebaseApplyLeftStopped(t *testing.T) {
	for name, stop := range map[string][]string{
		"git am":              {"am", "-3", ".git/topic.mbox"},
		"git am without 3way": {"am", ".git/topic.mbox"},
		"git rebase --apply":  {"rebase", "--apply", "main", "topic"},
	} {
		t.Run(name, func(t *testing.T) {
			o := newOracle(t)
			sides := [2]*mergeBuilder{}
			for i, side := range []string{"git", "ours"} {
				sides[i] = &mergeBuilder{o: o, dir: o.repoDir(side), clock: mergeClockStart}
				newOracleRepo(o, sides[i].dir)
				o.run(sides[i].dir, "config", "core.autocrlf", "false")
				applyConflictHistory(sides[i])
				if _, err := sides[i].dated().attempt(sides[i].dir, stop...); err == nil {
					t.Fatalf("git %v did not stop", stop)
				}
			}
			gitSide, ourSide := sides[0], sides[1]

			r := o.openRepo(ourSide.dir)
			if _, err := Merge(t.Context(), r, "topic", MergeOptions{}); !errors.Is(err, ErrMergeInProgress) {
				t.Fatalf("Merge over a stopped %s: %v", name, err)
			}
			if _, err := ContinueRebase(t.Context(), r, RebaseOptions{}); !errors.Is(err, ErrRebaseApplyInProgress) {
				t.Fatalf("ContinueRebase over a stopped %s: %v", name, err)
			}
			abort := []string{"rebase", "--abort"}
			if stop[0] == "am" {
				abort = []string{"am", "--abort"}
			}
			gitSide.git(abort...)
			if err := AbortOperation(t.Context(), r); err != nil {
				t.Fatalf("AbortOperation returned error %v", err)
			}

			if got, want := stoppedByGitState(ourSide), stoppedByGitState(gitSide); got != want {
				t.Fatalf("the abort differs from git: %s", sectionDiff(got, want))
			}
		})
	}
}

func TestOracleWeAbortAnInteractiveRebaseWhoseTodoWeCannotRun(t *testing.T) {
	o := newOracle(t)
	sides := [2]*mergeBuilder{}
	for i, side := range []string{"git", "ours"} {
		b := &mergeBuilder{o: o, dir: o.repoDir(side), clock: mergeClockStart}
		sides[i] = b
		newOracleRepo(o, b.dir)
		o.run(b.dir, "config", "core.autocrlf", "false")
		commits := interactiveHistory(b)
		o.write(b.dir, ".git/todo.txt", "edit "+commits[0]+" a\nexec echo run\nlabel here\npick "+commits[1]+" b\nbreak\nfixup -C "+commits[2]+" c\n")
		runner := b.dated()
		runner.env = append(slices.Clone(runner.env), "GIT_SEQUENCE_EDITOR=cp .git/todo.txt", "GIT_EDITOR=true")
		runner.run(b.dir, "rebase", "-i", "main")
	}
	gitSide, ourSide := sides[0], sides[1]
	todo := o.read(ourSide.dir, ".git/rebase-merge/git-rebase-todo")

	r := o.openRepo(ourSide.dir)
	if _, err := ContinueRebase(t.Context(), r, RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}); !errors.Is(err, ErrRebaseStepUnsupported) {
		t.Fatalf("ContinueRebase returned %v", err)
	}
	if got := o.read(ourSide.dir, ".git/rebase-merge/git-rebase-todo"); got != todo {
		t.Fatalf("the todo changed from %q to %q", todo, got)
	}
	gitSide.git("rebase", "--abort")
	if err := AbortOperation(t.Context(), r); err != nil {
		t.Fatalf("AbortOperation returned error %v", err)
	}

	if got, want := stoppedByGitState(ourSide), stoppedByGitState(gitSide); got != want {
		t.Fatalf("the abort differs from git: %s", sectionDiff(got, want))
	}
}
