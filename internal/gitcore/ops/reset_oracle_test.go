//go:build oracle

package ops

import (
	"strings"
	"testing"
	"time"
)

type resetScenario struct {
	name   string
	setup  func(b *mergeBuilder)
	target string
	args   []string
	mode   ResetMode
	paths  []string
}

func resetHistory(b *mergeBuilder) {
	f := lines("f", 10)
	b.commit("base", map[string]string{"keep": "keep\n", "f": f, "dir/old": "old\n"})
	b.commit("next", map[string]string{"f": editLine(f, 1, "NEXT"), "dir/new": "new\n", "dir/old": ""})
}

func conflictedHistory(b *mergeBuilder) {
	f := lines("f", 10)
	forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": editLine(f, 4, "THEIRS")})(b)
	_, _ = b.dated().attempt(b.dir, "merge", "--no-edit", "feature")
}

func unbornHead(b *mergeBuilder) {
	resetHistory(b)
	b.git("symbolic-ref", "HEAD", "refs/heads/unborn")
}

func resetScenarios() []resetScenario {
	f := lines("f", 10)
	return []resetScenario{
		{name: "soft one back", setup: resetHistory, target: "HEAD~1", args: []string{"--soft"}, mode: ResetSoft},
		{name: "mixed one back", setup: resetHistory, target: "HEAD~1"},
		{name: "hard one back", setup: resetHistory, target: "HEAD~1", args: []string{"--hard"}, mode: ResetHard},
		{name: "hard to head with a dirty file", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.o.write(b.dir, "f", editLine(f, 7, "DIRTY"))
		}, target: "HEAD", args: []string{"--hard"}, mode: ResetHard},
		{name: "hard to head with a deleted file", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.o.remove(b.dir, "keep")
		}, target: "HEAD", args: []string{"--hard"}, mode: ResetHard},
		{name: "hard keeps untracked files", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.o.write(b.dir, "spare", "spare\n")
		}, target: "HEAD~1", args: []string{"--hard"}, mode: ResetHard},
		{name: "hard drops staged changes", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.write(map[string]string{"f": editLine(f, 2, "STAGED"), "added": "added\n"})
		}, target: "HEAD", args: []string{"--hard"}, mode: ResetHard},
		{name: "mixed unstages an addition", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.write(map[string]string{"added": "added\n"})
		}, target: "HEAD"},
		{name: "mixed one back with a dirty file", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.o.write(b.dir, "f", editLine(f, 7, "DIRTY"))
		}, target: "HEAD~1"},
		{name: "soft in the middle of a merge", setup: conflictedHistory, target: "HEAD", args: []string{"--soft"}, mode: ResetSoft},
		{name: "mixed in the middle of a merge", setup: conflictedHistory, target: "HEAD"},
		{name: "hard in the middle of a merge", setup: conflictedHistory, target: "HEAD", args: []string{"--hard"}, mode: ResetHard},
		{name: "hard to the other branch", setup: conflictedHistory, target: "feature", args: []string{"--hard"}, mode: ResetHard},
		{name: "detached hard one back", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.git("checkout", "-q", "--detach")
		}, target: "HEAD~1", args: []string{"--hard"}, mode: ResetHard},
		{name: "to a tag", setup: func(b *mergeBuilder) {
			resetHistory(b)
			b.git("tag", "start", "HEAD~1")
		}, target: "start", args: []string{"--hard"}, mode: ResetHard},
		{name: "paths from the previous commit", setup: resetHistory, target: "HEAD~1", paths: []string{"f"}},
		{name: "paths of a whole directory", setup: resetHistory, target: "HEAD~1", paths: []string{"dir"}},
		{name: "paths of a conflicted file", setup: conflictedHistory, target: "HEAD", paths: []string{"f"}},
		{name: "paths with a soft reset", setup: resetHistory, target: "HEAD~1", args: []string{"--soft"}, mode: ResetSoft, paths: []string{"f"}},
		{name: "paths with a hard reset", setup: resetHistory, target: "HEAD~1", args: []string{"--hard"}, mode: ResetHard, paths: []string{"f"}},
		{name: "unknown target", setup: resetHistory, target: "nope", args: []string{"--hard"}, mode: ResetHard},
		{name: "unborn branch soft", setup: unbornHead, target: "main", args: []string{"--soft"}, mode: ResetSoft},
		{name: "unborn branch mixed", setup: unbornHead, target: "main"},
		{name: "unborn branch hard", setup: unbornHead, target: "main", args: []string{"--hard"}, mode: ResetHard},
	}
}

func TestOracleResetLeavesTheRepositoryAsGitResetDoes(t *testing.T) {
	for _, s := range resetScenarios() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			sides := [2]*mergeBuilder{}
			for i, name := range []string{"git", "ours"} {
				dir := o.repoDir(name)
				newOracleRepo(o, dir)
				o.run(dir, "config", "core.autocrlf", "false")
				sides[i] = &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
				s.setup(sides[i])
			}
			gitSide, ourSide := sides[0], sides[1]

			args := append([]string{"reset", "-q"}, s.args...)
			args = append(args, s.target)
			if len(s.paths) > 0 {
				args = append(append(args, "--"), s.paths...)
			}
			_, gitErr := gitSide.dated().attempt(gitSide.dir, args...)
			ourSide.dated()
			opts := ResetOptions{Mode: s.mode, Paths: s.paths, When: time.Unix(ourSide.clock, 0).UTC()}
			_, ourErr := Reset(t.Context(), o.openRepo(ourSide.dir), s.target, opts)
			if (gitErr != nil) != (ourErr != nil) {
				t.Fatalf("git failed = %v, ours = %v", gitErr, ourErr)
			}

			refused := ourErr != nil
			if got, want := mergeStateOf(ourSide, refused), mergeStateOf(gitSide, refused); got != want {
				t.Fatalf("repository differs from %s: %s", strings.TrimSpace(o.run(gitSide.dir, "--version")), sectionDiff(got, want))
			}
		})
	}
}
