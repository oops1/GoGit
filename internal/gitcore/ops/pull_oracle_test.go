//go:build oracle

package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type pullScenario struct {
	name   string
	local  map[string]string
	remote map[string]string
	rebase bool
}

func pullScenarios() []pullScenario {
	f := lines("f", 10)
	return []pullScenario{
		{name: "diverged cleanly", local: map[string]string{"f": editLine(f, 0, "LOCAL")}, remote: map[string]string{"f": editLine(f, 9, "REMOTE")}},
		{name: "conflict", local: map[string]string{"f": editLine(f, 4, "LOCAL")}, remote: map[string]string{"f": editLine(f, 4, "REMOTE")}},
		{name: "behind only", remote: map[string]string{"g": "new\n"}},
		{name: "rebased cleanly", rebase: true, local: map[string]string{"f": editLine(f, 0, "LOCAL")}, remote: map[string]string{"f": editLine(f, 9, "REMOTE")}},
		{name: "rebase conflict", rebase: true, local: map[string]string{"f": editLine(f, 4, "LOCAL")}, remote: map[string]string{"f": editLine(f, 4, "REMOTE")}},
		{name: "rebase of a branch behind", rebase: true, remote: map[string]string{"g": "new\n"}},
	}
}

func buildPullSide(t *testing.T, o *oracle, s pullScenario) (*mergeBuilder, string) {
	t.Helper()
	root := o.repoDir("side")
	up := filepath.ToSlash(root) + "/up.git"
	o.run(root, "init", "-q", "--bare", "-b", "main", up)
	seed := &mergeBuilder{o: o, dir: filepath.Join(root, "seed"), clock: mergeClockStart}
	o.run(root, "clone", "-q", up, "seed")
	o.run(seed.dir, "config", "core.autocrlf", "false")
	o.run(seed.dir, "config", "user.name", "oracle")
	o.run(seed.dir, "config", "user.email", "oracle@example.com")
	seed.commit("base", map[string]string{"f": lines("f", 10)})
	seed.git("push", "-q", "origin", "main")
	work := &mergeBuilder{o: o, dir: filepath.Join(root, "work"), clock: seed.clock}
	o.run(root, "clone", "-q", up, "work")
	o.run(work.dir, "config", "core.autocrlf", "false")
	o.run(work.dir, "config", "user.name", "oracle")
	o.run(work.dir, "config", "user.email", "oracle@example.com")
	o.run(work.dir, "config", "pull.rebase", strconv.FormatBool(s.rebase))
	if s.remote != nil {
		seed.clock = work.clock
		seed.commit("remote", s.remote)
		seed.git("push", "-q", "origin", "main")
		work.clock = seed.clock
	}
	if s.local != nil {
		work.commit("local", s.local)
	}
	return work, filepath.ToSlash(root)
}

func pullStateOf(b *mergeBuilder, root string) string {
	b.o.t.Helper()
	var out []string
	add := func(label, text string) { out = append(out, "== "+label+"\n"+text) }
	add("message", b.o.run(b.dir, "log", "-1", "--format=%B%n%an %ae %ad%n%cn %ce %cd"))
	add("parents", strconv.Itoa(len(strings.Fields(b.o.run(b.dir, "log", "-1", "--format=%P")))))
	add("reflog", b.o.run(b.dir, "reflog", "-1", "--format=%gs", "HEAD"))
	add("index", b.o.run(b.dir, "ls-files", "-s"))
	add("status", b.o.run(b.dir, "status", "--porcelain", "-uall"))
	add("head", b.o.run(b.dir, "rev-parse", "--symbolic-full-name", "HEAD"))
	for _, name := range []string{mergeHeadFile, mergeMsgFile, mergeModeFile, autoMergeFile, rebaseHeadFile, rebasePath(rebaseTodo), rebasePath(rebaseDone), rebasePath(rebaseHeadName)} {
		data, err := b.o.attempt(b.dir, "rev-parse", "--git-path", name)
		if err != nil {
			b.o.t.Fatal(err)
		}
		add(name, readIfPresent(filepath.Join(b.dir, strings.TrimSpace(data))))
	}
	add("file f", readIfPresent(filepath.Join(b.dir, "f")))
	return strings.ReplaceAll(strings.Join(out, "\n"), root, "<root>")
}

func readIfPresent(path string) string {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "<none>"
	}
	return string(data)
}

func TestOraclePullMergesAsGitPullDoes(t *testing.T) {
	for _, s := range pullScenarios() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			gitSide, gitRoot := buildPullSide(t, o, s)
			ourSide, ourRoot := buildPullSide(t, o, s)

			_, gitErr := gitSide.dated().attempt(gitSide.dir, "pull")
			ourSide.dated()
			result, ourErr := Pull(t.Context(), o.openRepo(ourSide.dir), PullOptions{When: time.Unix(ourSide.clock, 0).UTC()})
			if (gitErr != nil) != (ourErr != nil || len(result.Merge.Conflicts) > 0 || len(result.Rebase.Conflicts) > 0) {
				t.Fatalf("git: %v, ours: %+v, %v", gitErr, result, ourErr)
			}

			got := pullStateOf(ourSide, ourRoot)
			if want := pullStateOf(gitSide, gitRoot); got != want {
				t.Fatalf("repository differs from %s: %s", strings.TrimSpace(o.run(gitSide.dir, "--version")), sectionDiff(got, want))
			}
		})
	}
}
