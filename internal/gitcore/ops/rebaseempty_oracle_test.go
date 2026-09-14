//go:build oracle

package ops

import (
	"slices"
	"strings"
	"testing"
	"time"
)

type emptyRebaseScenario struct {
	name    string
	actions []string
	stops   bool
}

func emptyRebaseHistory(b *mergeBuilder) []string {
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f})
	b.git("branch", "topic")
	b.commit("main f and g", map[string]string{"f": editLine(f, 1, "MAIN"), "g": "g\n"})
	b.git("checkout", "-q", "topic")
	var commits []string
	for _, step := range []struct {
		message string
		files   map[string]string
	}{
		{"started empty", nil},
		{"only g", map[string]string{"g": "g\n"}},
		{"topic h", map[string]string{"h": "h\n"}},
	} {
		b.commit(step.message, step.files)
		commits = append(commits, strings.TrimSpace(b.o.run(b.dir, "rev-parse", "HEAD")))
	}
	return commits
}

func TestOracleARebaseKeepsCommitsThatStartedEmptyLikeGit(t *testing.T) {
	for _, s := range []emptyRebaseScenario{
		{name: "a plain rebase"},
		{name: "picks", actions: []string{actionPick, actionPick, actionPick}},
		{name: "an edit of the commit that becomes empty", actions: []string{actionPick, actionEdit, actionPick}, stops: true},
		{name: "an edit of the commit that started empty", actions: []string{actionEdit, actionPick, actionPick}, stops: true},
		{name: "a reword of the commit that becomes empty", actions: []string{actionPick, actionReword, actionPick}},
	} {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			gitSide := &mergeBuilder{o: o, dir: o.repoDir("git"), clock: mergeClockStart}
			ourSide := &mergeBuilder{o: o, dir: o.repoDir("ours"), clock: mergeClockStart}
			var commits []string
			for _, side := range []*mergeBuilder{gitSide, ourSide} {
				newOracleRepo(o, side.dir)
				o.run(side.dir, "config", "core.autocrlf", "false")
				commits = emptyRebaseHistory(side)
			}

			runner := gitSide.dated()
			runner.env = append(slices.Clone(runner.env), "GIT_EDITOR=true")
			args := []string{"rebase", "main"}
			if s.actions != nil {
				o.write(gitSide.dir, ".git/todo.txt", todoLines(s.actions, commits))
				runner.env = append(runner.env, "GIT_SEQUENCE_EDITOR=cp .git/todo.txt")
				args = []string{"rebase", "-i", "--empty=drop", "main"}
			}
			runner.run(gitSide.dir, args...)
			if s.stops {
				runner = gitSide.dated()
				runner.env = append(slices.Clone(runner.env), "GIT_EDITOR=true")
				runner.run(gitSide.dir, "rebase", "--continue")
			}

			ourSide.dated()
			opts := RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}
			if s.actions != nil {
				opts.Todo = ourTodo(s.actions, commits)
			}
			result, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", opts)
			if err != nil {
				t.Fatalf("Rebase returned error %v", err)
			}
			if !result.Finished() {
				if s.actions[1] == actionReword {
					if _, err := ContinueRebase(t.Context(), o.openRepo(ourSide.dir), opts); err != nil {
						t.Fatalf("ContinueRebase returned error %v", err)
					}
				} else {
					ourSide.dated()
					if _, err := ContinueRebase(t.Context(), o.openRepo(ourSide.dir), RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
						t.Fatalf("ContinueRebase returned error %v", err)
					}
				}
			} else if s.stops {
				t.Fatalf("result = %+v", result)
			}

			if got, want := upToDateState(ourSide), upToDateState(gitSide); got != want {
				t.Fatalf("the rebase differs from git: %s", sectionDiff(got, want))
			}
		})
	}
}
