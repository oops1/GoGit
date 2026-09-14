//go:build oracle

package ops

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func upToDateHistory(b *mergeBuilder) []string {
	b.commit("base", map[string]string{"f": lines("f", 10)})
	b.git("checkout", "-q", "-b", "topic")
	var commits []string
	for _, name := range []string{"a", "b", "c"} {
		b.commit("topic "+name, map[string]string{name: name + "\n"})
		commits = append(commits, strings.TrimSpace(b.o.run(b.dir, "rev-parse", "HEAD")))
	}
	return commits
}

func upToDateState(b *mergeBuilder) string {
	b.o.t.Helper()
	return strings.Join([]string{
		b.o.run(b.dir, "log", "--format=%H %T %an %ad %cn %cd%n%B"),
		b.o.run(b.dir, "symbolic-ref", "HEAD"),
		b.o.run(b.dir, "status", "--porcelain"),
	}, "\n== ")
}

func TestOracleAnInteractiveRebaseOfAnUpToDateBranchRunsItsSteps(t *testing.T) {
	for _, s := range []interactiveScenario{
		{name: "only picks", actions: []string{actionPick, actionPick, actionPick}},
		{name: "a reword after a pick", actions: []string{actionPick, actionReword, actionPick}, weStop: true},
		{name: "a squash", actions: []string{actionPick, actionSquash, actionPick}},
		{name: "a dropped first commit", actions: []string{actionDrop, actionPick, actionPick}},
		{name: "a fixup at the end", actions: []string{actionPick, actionPick, actionFixup}},
	} {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			gitSide := &mergeBuilder{o: o, dir: o.repoDir("git"), clock: mergeClockStart}
			ourSide := &mergeBuilder{o: o, dir: o.repoDir("ours"), clock: mergeClockStart}
			var commits []string
			for _, side := range []*mergeBuilder{gitSide, ourSide} {
				newOracleRepo(o, side.dir)
				o.run(side.dir, "config", "core.autocrlf", "false")
				commits = upToDateHistory(side)
			}

			o.write(gitSide.dir, ".git/todo.txt", todoLines(s.actions, commits))
			runner := gitSide.dated()
			runner.env = append(slices.Clone(runner.env), "GIT_SEQUENCE_EDITOR=cp .git/todo.txt", "GIT_EDITOR=true")
			runner.run(gitSide.dir, "rebase", "-i", "main")

			ourSide.dated()
			when := time.Unix(ourSide.clock, 0).UTC()
			result, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", RebaseOptions{When: when, Todo: ourTodo(s.actions, commits)})
			if err != nil || result.Finished() == s.weStop {
				t.Fatalf("result = %+v, %v", result, err)
			}
			if s.weStop {
				if _, err := ContinueRebase(t.Context(), o.openRepo(ourSide.dir), RebaseOptions{When: when}); err != nil {
					t.Fatalf("ContinueRebase returned error %v", err)
				}
			}

			if got, want := upToDateState(ourSide), upToDateState(gitSide); got != want {
				t.Fatalf("the rebase differs from git: %s", sectionDiff(got, want))
			}
		})
	}
}
