//go:build oracle

package ops

import (
	"slices"
	"strings"
	"testing"
	"time"
)

type editStopScenario struct {
	name  string
	git   func(b *mergeBuilder)
	ours  func(t *testing.T, b *mergeBuilder)
	fails bool
}

func ourCommitAt(t *testing.T, b *mergeBuilder, opts CommitOptions) {
	t.Helper()
	b.dated()
	opts.When = time.Unix(b.clock, 0).UTC()
	if _, err := Commit(t.Context(), b.o.openRepo(b.dir), opts); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

func stageChange(b *mergeBuilder) {
	b.o.write(b.dir, "x", "changed while editing\n")
	b.o.run(b.dir, "add", "x")
}

func editStopScenarios() []editStopScenario {
	return []editStopScenario{
		{name: "nothing changed", git: func(*mergeBuilder) {}, ours: func(*testing.T, *mergeBuilder) {}},
		{name: "a staged change", git: stageChange, ours: func(_ *testing.T, b *mergeBuilder) { stageChange(b) }},
		{
			name: "split into two commits",
			git: func(b *mergeBuilder) {
				b.o.run(b.dir, "reset", "-q", "HEAD^")
				b.o.run(b.dir, "add", "x")
				b.git("commit", "-q", "-m", "only x")
				b.o.run(b.dir, "add", "y")
				b.git("commit", "-q", "-m", "only y")
			},
			ours: func(t *testing.T, b *mergeBuilder) {
				b.o.run(b.dir, "reset", "-q", "HEAD^")
				b.o.run(b.dir, "add", "x")
				ourCommitAt(t, b, CommitOptions{Message: "only x"})
				b.o.run(b.dir, "add", "y")
				ourCommitAt(t, b, CommitOptions{Message: "only y"})
			},
		},
		{
			name: "amended before continuing",
			git:  func(b *mergeBuilder) { b.git("commit", "-q", "--amend", "-m", "amended") },
			ours: func(t *testing.T, b *mergeBuilder) {
				ourCommitAt(t, b, CommitOptions{Message: "amended", Amend: true})
			},
		},
		{
			name: "amended and then staged",
			git: func(b *mergeBuilder) {
				b.git("commit", "-q", "--amend", "-m", "amended")
				stageChange(b)
			},
			ours: func(t *testing.T, b *mergeBuilder) {
				ourCommitAt(t, b, CommitOptions{Message: "amended", Amend: true})
				stageChange(b)
			},
			fails: true,
		},
	}
}

func editStopHistory(b *mergeBuilder) string {
	b.commit("base", map[string]string{"f": lines("f", 10), "keep": "keep\n"})
	b.git("branch", "topic")
	b.commit("main f", map[string]string{"f": editLine(lines("f", 10), 0, "MAIN")})
	b.git("checkout", "-q", "topic")
	b.commit("x and y", map[string]string{"x": "x\n", "y": "y\n"})
	edited := strings.TrimSpace(b.o.run(b.dir, "rev-parse", "HEAD"))
	b.commit("topic c", map[string]string{"c": "c\n"})
	picked := strings.TrimSpace(b.o.run(b.dir, "rev-parse", "HEAD"))
	return "edit " + edited + " x and y\npick " + picked + " topic c\n"
}

func editStopState(b *mergeBuilder) string {
	b.o.t.Helper()
	var out []string
	out = append(out, b.o.run(b.dir, "log", "-5", "--format=%H %T %an %ad %cn %cd %s"))
	out = append(out, b.o.run(b.dir, "status", "--porcelain", "-uno"))
	branch, _ := b.o.attempt(b.dir, "symbolic-ref", "-q", "HEAD")
	out = append(out, branch)
	if _, err := b.o.attempt(b.dir, "rev-parse", "-q", "--verify", "REBASE_HEAD"); err == nil {
		out = append(out, "rebasing")
	}
	return strings.Join(out, "\n== ")
}

func TestOracleContinuingAnEditStopMatchesGit(t *testing.T) {
	for _, s := range editStopScenarios() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			gitSide := &mergeBuilder{o: o, dir: o.repoDir("git"), clock: mergeClockStart}
			ourSide := &mergeBuilder{o: o, dir: o.repoDir("ours"), clock: mergeClockStart}
			var todo string
			for _, side := range []*mergeBuilder{gitSide, ourSide} {
				newOracleRepo(o, side.dir)
				o.run(side.dir, "config", "core.autocrlf", "false")
				todo = editStopHistory(side)
			}

			o.write(gitSide.dir, ".git/todo.txt", todo)
			runner := gitSide.dated()
			runner.env = append(slices.Clone(runner.env), "GIT_SEQUENCE_EDITOR=cp .git/todo.txt", "GIT_EDITOR=true")
			runner.run(gitSide.dir, "rebase", "-i", "main")
			s.git(gitSide)
			runner = gitSide.dated()
			runner.env = append(slices.Clone(runner.env), "GIT_EDITOR=true")
			_, gitErr := runner.attempt(gitSide.dir, "rebase", "--continue")

			ourSide.dated()
			steps, err := parseTodo(todo)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC(), Todo: steps}); err != nil {
				t.Fatalf("Rebase returned error %v", err)
			}
			s.ours(t, ourSide)
			ourSide.dated()
			_, ourErr := ContinueRebase(t.Context(), o.openRepo(ourSide.dir), RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()})

			if (gitErr != nil) != s.fails || (ourErr != nil) != s.fails {
				t.Fatalf("git continue: %v, our continue: %v", gitErr, ourErr)
			}
			if got, want := editStopState(ourSide), editStopState(gitSide); got != want {
				t.Fatalf("the rebase differs from git: %s", sectionDiff(got, want))
			}
		})
	}
}
