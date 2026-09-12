//go:build oracle

package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type interactiveScenario struct {
	name     string
	actions  []string
	gitStops bool
	weStop   bool
}

func interactiveScenarios() []interactiveScenario {
	return []interactiveScenario{
		{name: "three picks", actions: []string{actionPick, actionPick, actionPick}},
		{name: "a dropped commit", actions: []string{actionPick, actionDrop, actionPick}},
		{name: "only drops", actions: []string{actionDrop, actionDrop, actionDrop}},
		{name: "a squash", actions: []string{actionPick, actionSquash, actionPick}},
		{name: "a fixup", actions: []string{actionPick, actionFixup, actionPick}},
		{name: "a squash and a fixup in a row", actions: []string{actionPick, actionSquash, actionFixup}},
		{name: "a reword", actions: []string{actionPick, actionReword, actionPick}, weStop: true},
		{name: "an edit", actions: []string{actionEdit, actionPick, actionPick}, gitStops: true, weStop: true},
	}
}

func interactiveHistory(b *mergeBuilder) []string {
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
	b.git("branch", "topic")
	b.commit("main f", map[string]string{"f": editLine(f, 0, "MAIN")})
	b.git("checkout", "-q", "topic")
	var commits []string
	for _, name := range []string{"a", "b", "c"} {
		b.commit("topic "+name, map[string]string{name: name + "\n"})
		commits = append(commits, strings.TrimSpace(b.o.run(b.dir, "rev-parse", "HEAD")))
	}
	return commits
}

func todoLines(actions, commits []string) string {
	var out strings.Builder
	for i, action := range actions {
		out.WriteString(action + " " + commits[i] + " topic\n")
	}
	return out.String()
}

func interactiveStateOf(b *mergeBuilder) string {
	b.o.t.Helper()
	var out []string
	add := func(label, text string) { out = append(out, "== "+label+"\n"+text) }
	history, _ := b.o.attempt(b.dir, "log", "-6", "--format=%T%n%an %ae %ad%n%B")
	add("history", history)
	add("branch", b.o.run(b.dir, "rev-parse", "--symbolic-full-name", "HEAD"))
	add("index", b.o.run(b.dir, "ls-files", "-s"))
	add("status", b.o.run(b.dir, "status", "--porcelain", "-uall"))
	for _, name := range []string{"a", "b", "c", "f", "keep"} {
		text, err := b.o.attempt(b.dir, "cat-file", "-p", ":"+name)
		if err != nil {
			text = "<none>"
		}
		add("file "+name, text)
	}
	return strings.Join(out, "\n")
}

func ourTodo(actions, commits []string) []RebaseStep {
	var todo []RebaseStep
	for i, action := range actions {
		id, err := hash.Parse(commits[i])
		if err != nil {
			panic(err)
		}
		todo = append(todo, RebaseStep{Action: action, Commit: id, Subject: "topic"})
	}
	return todo
}

func TestOracleAnInteractiveRebaseEndsWhereGitsDoes(t *testing.T) {
	for _, s := range interactiveScenarios() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			sides := [2]*mergeBuilder{}
			commits := [2][]string{}
			for i, name := range []string{"git", "ours"} {
				dir := o.repoDir(name)
				newOracleRepo(o, dir)
				o.run(dir, "config", "core.autocrlf", "false")
				sides[i] = &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
				commits[i] = interactiveHistory(sides[i])
			}
			gitSide, ourSide := sides[0], sides[1]
			if !slices.Equal(commits[0], commits[1]) {
				t.Fatalf("the two histories differ: %v and %v", commits[0], commits[1])
			}

			o.write(gitSide.dir, ".git/todo.txt", todoLines(s.actions, commits[0]))
			gitSide.clock += 60
			gitRebase(t, gitSide, s)
			ourSide.clock += 60
			opts := RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC(), Todo: ourTodo(s.actions, commits[1])}
			result, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", opts)
			if err != nil {
				t.Fatalf("Rebase returned error %v", err)
			}
			if result.Finished() == s.weStop {
				t.Fatalf("result = %+v", result)
			}
			if s.weStop {
				if _, err := ContinueRebase(t.Context(), o.openRepo(ourSide.dir), RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
					t.Fatalf("ContinueRebase returned error %v", err)
				}
			}

			if got, want := interactiveStateOf(ourSide), interactiveStateOf(gitSide); got != want {
				t.Fatalf("the rebase differs from %s: %s", strings.TrimSpace(o.run(gitSide.dir, "--version")), sectionDiff(got, want))
			}
		})
	}
}

func gitRebaseStart(t *testing.T, b *mergeBuilder, s interactiveScenario) {
	t.Helper()
	runner := b.dated()
	runner.env = append(slices.Clone(runner.env), "GIT_SEQUENCE_EDITOR=cp .git/todo.txt", "GIT_EDITOR=true")
	if _, err := runner.attempt(b.dir, "rebase", "-i", "main"); err != nil && !s.gitStops {
		t.Fatalf("git rebase -i: %v", err)
	}
}

func gitRebaseContinue(t *testing.T, b *mergeBuilder) {
	t.Helper()
	runner := b.dated()
	runner.env = append(slices.Clone(runner.env), "GIT_EDITOR=true")
	if _, err := runner.attempt(b.dir, "rebase", "--continue"); err != nil {
		t.Fatalf("git rebase --continue: %v", err)
	}
}

func gitRebase(t *testing.T, b *mergeBuilder, s interactiveScenario) {
	t.Helper()
	gitRebaseStart(t, b, s)
	if s.gitStops {
		gitRebaseContinue(t, b)
	}
}

func TestOracleGitFinishesTheInteractiveRebaseWeStopped(t *testing.T) {
	o := newOracle(t)
	gitSide := &mergeBuilder{o: o, dir: o.repoDir("git"), clock: mergeClockStart}
	ourSide := &mergeBuilder{o: o, dir: o.repoDir("ours"), clock: mergeClockStart}
	for _, side := range []*mergeBuilder{gitSide, ourSide} {
		newOracleRepo(o, side.dir)
		o.run(side.dir, "config", "core.autocrlf", "false")
	}
	gitCommits := interactiveHistory(gitSide)
	ourCommits := interactiveHistory(ourSide)
	actions := []string{actionEdit, actionPick, actionPick}

	o.write(gitSide.dir, ".git/todo.txt", todoLines(actions, gitCommits))
	gitSide.clock += 60
	gitRebase(t, gitSide, interactiveScenario{actions: actions, gitStops: true})

	ourSide.clock += 60
	result, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", RebaseOptions{
		When: time.Unix(ourSide.clock, 0).UTC(),
		Todo: ourTodo(actions, ourCommits),
	})
	if err != nil || result.Finished() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	runner := ourSide.dated()
	runner.env = append(slices.Clone(runner.env), "GIT_EDITOR=true")
	if _, err := runner.attempt(ourSide.dir, "rebase", "--continue"); err != nil {
		t.Fatalf("git rebase --continue over our state: %v", err)
	}

	if got, want := interactiveStateOf(ourSide), interactiveStateOf(gitSide); got != want {
		t.Fatalf("the rebase differs from %s: %s", strings.TrimSpace(o.run(gitSide.dir, "--version")), sectionDiff(got, want))
	}
}

func TestOracleWeFinishTheInteractiveRebaseGitStopped(t *testing.T) {
	o := newOracle(t)
	gitSide := &mergeBuilder{o: o, dir: o.repoDir("git"), clock: mergeClockStart}
	ourSide := &mergeBuilder{o: o, dir: o.repoDir("ours"), clock: mergeClockStart}
	for _, side := range []*mergeBuilder{gitSide, ourSide} {
		newOracleRepo(o, side.dir)
		o.run(side.dir, "config", "core.autocrlf", "false")
	}
	gitCommits := interactiveHistory(gitSide)
	ourCommits := interactiveHistory(ourSide)
	actions := []string{actionEdit, actionPick, actionPick}

	for i, side := range []*mergeBuilder{gitSide, ourSide} {
		commits := gitCommits
		if i == 1 {
			commits = ourCommits
		}
		o.write(side.dir, ".git/todo.txt", todoLines(actions, commits))
		side.clock += 60
		gitRebaseStart(t, side, interactiveScenario{actions: actions, gitStops: true})
	}
	gitRebaseContinue(t, gitSide)

	if _, err := ContinueRebase(t.Context(), o.openRepo(ourSide.dir), RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
		t.Fatalf("ContinueRebase over git's state returned error %v", err)
	}

	if got, want := interactiveStateOf(ourSide), interactiveStateOf(gitSide); got != want {
		t.Fatalf("the rebase differs from %s: %s", strings.TrimSpace(o.run(gitSide.dir, "--version")), sectionDiff(got, want))
	}
}
