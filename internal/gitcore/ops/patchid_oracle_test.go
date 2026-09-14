//go:build oracle

package ops

import (
	"bytes"
	"crypto/sha1"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

func gitStablePatchID(t *testing.T, b *mergeBuilder, commit string) string {
	t.Helper()
	patch := b.o.run(b.dir, "diff-tree", "-p", "--full-index", commit)
	cmd := exec.CommandContext(t.Context(), "git", "patch-id", "--stable")
	cmd.Dir, cmd.Env, cmd.Stdin = b.dir, b.o.env, strings.NewReader(patch)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git patch-id: %v", err)
	}
	id, _, _ := strings.Cut(out.String(), " ")
	return id
}

func TestOracleOurPatchIDsAreGitsStablePatchIDs(t *testing.T) {
	o := newOracle(t)
	b := &mergeBuilder{o: o, dir: o.repoDir("patch-id"), clock: mergeClockStart}
	newOracleRepo(o, b.dir)
	o.run(b.dir, "config", "core.autocrlf", "false")
	f := lines("f", 20)
	b.commit("base", map[string]string{
		"f":                    f,
		"gone":                 "going away\n",
		"bin":                  "binary\x00one\n",
		"no newline":           "last line without newline",
		"script":               "#!/bin/sh\necho one\n",
		"dir with space/inner": "\tindented with a tab\n",
	})
	b.commit("two hunks, an added and a deleted file", map[string]string{
		"f":           editLine(editLine(f, 2, "SECOND  LINE"), 15, "FIFTEENTH"),
		"gone":        "",
		"added/child": "a new file\nwith two lines\n",
	})
	b.commit("binary", map[string]string{"bin": "binary\x00two\n"})
	b.commit("missing newline and spaces", map[string]string{
		"no newline":           "last line changed without newline",
		"dir with space/inner": "\tindented  with\ttabs and spaces \n",
	})
	o.write(b.dir, "script", "#!/bin/sh\necho two\n")
	o.run(b.dir, "add", "script")
	o.run(b.dir, "update-index", "--chmod=+x", "script")
	b.git("commit", "-q", "-m", "mode and content")

	store := mergeStore{db: (&testRepo{t: t, dir: b.dir, repo: o.openRepo(b.dir)}).db()}
	for _, rev := range []string{"HEAD~3", "HEAD~2", "HEAD~1", "HEAD"} {
		parent := revTree(t, b, rev+"^")
		tree := revTree(t, b, rev)
		id, err := patchID(t.Context(), store, parent, tree)
		if err != nil {
			t.Fatalf("patchID returned error %v", err)
		}
		files, err := diff.Trees(t.Context(), store, parent, tree, diff.Options{Context: diff.DefaultContext})
		if err != nil {
			t.Fatal(err)
		}
		var ours []string
		for _, file := range files {
			ours = append(ours, string(patchIDText(file)))
			if file.Binary {
				addPatchIDPart(&id, sha1.Sum(nil))
			}
		}
		if want := gitStablePatchID(t, b, rev); id.String() != want {
			t.Fatalf("patch id of %s = %s, git patch-id --stable = %s\npatch:\n%q\nours:\n%q", rev, id, want, o.run(b.dir, "diff-tree", "-p", "--full-index", rev), ours)
		}
	}
}

func revTree(t *testing.T, b *mergeBuilder, rev string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(strings.TrimSpace(b.o.run(b.dir, "rev-parse", rev+"^{tree}")))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func appliedUpstreamHistory(b *mergeBuilder) {
	f, g := lines("f", 10), lines("g", 10)
	b.commit("base", map[string]string{"f": f, "g": g})
	b.git("branch", "topic")
	b.commit("main f line 8", map[string]string{"f": editLine(f, 8, "MAIN EIGHT")})
	b.write(map[string]string{"f": editLine(editLine(f, 8, "MAIN EIGHT"), 2, "TOPIC TWO")})
	b.git("commit", "-q", "--author", "Picker <picker@example.com>", "-m", "picked f line 2")
	b.commit("revert the picked line", map[string]string{"f": editLine(f, 8, "MAIN EIGHT")})
	b.commit("main g line 1", map[string]string{"g": editLine(g, 1, "SHARED ONE")})
	b.git("checkout", "-q", "topic")
	b.commit("topic f line 2", map[string]string{"f": editLine(f, 2, "TOPIC TWO")})
	b.commit("topic h", map[string]string{"h": "h\n"})
	b.commit("topic g line 1", map[string]string{"g": editLine(g, 1, "SHARED ONE")})
}

func TestOracleARebaseSkipsCommitsAlreadyAppliedUpstreamLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := &mergeBuilder{o: o, dir: o.repoDir("git"), clock: mergeClockStart}
	ourSide := &mergeBuilder{o: o, dir: o.repoDir("ours"), clock: mergeClockStart}
	for _, side := range []*mergeBuilder{gitSide, ourSide} {
		newOracleRepo(o, side.dir)
		o.run(side.dir, "config", "core.autocrlf", "false")
		appliedUpstreamHistory(side)
	}

	runner := &oracle{t: t, home: o.home, env: append(slices.Clone(o.env), "GIT_SEQUENCE_EDITOR=cat >.git/captured.txt <")}
	if _, err := runner.attempt(gitSide.dir, "rebase", "-i", "main"); err != nil {
		t.Fatalf("git rebase -i: %v", err)
	}
	var gitPicks []string
	for line := range strings.Lines(o.read(gitSide.dir, ".git/captured.txt")) {
		if fields := strings.Fields(line); len(fields) > 1 && fields[0] == actionPick {
			gitPicks = append(gitPicks, fields[1])
		}
	}
	o.run(gitSide.dir, "reset", "-q", "--hard", "ORIG_HEAD")
	planned, err := PlannedRebase(t.Context(), o.openRepo(ourSide.dir), "main")
	if err != nil {
		t.Fatalf("PlannedRebase returned error %v", err)
	}
	if len(planned) != len(gitPicks) {
		t.Fatalf("planned %+v, git picks %v", planned, gitPicks)
	}
	for i, step := range planned {
		if !strings.HasPrefix(step.Commit.String(), gitPicks[i]) {
			t.Fatalf("planned %+v, git picks %v", planned, gitPicks)
		}
	}

	gitSide.dated().run(gitSide.dir, "rebase", "main")
	ourSide.dated()
	if _, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}); err != nil {
		t.Fatalf("Rebase returned error %v", err)
	}

	if got, want := upToDateState(ourSide), upToDateState(gitSide); got != want {
		t.Fatalf("the rebase differs from git: %s", sectionDiff(got, want))
	}
	if !slices.Contains(strings.Split(o.run(ourSide.dir, "log", "--format=%s"), "\n"), "topic h") {
		t.Fatal("the commit that was not upstream is gone")
	}
}
