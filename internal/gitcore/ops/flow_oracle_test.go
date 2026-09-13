//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const flowOracleStamp = 1700050000

func flowOracleSide(t *testing.T, o *oracle) *mergeBuilder {
	t.Helper()
	root := o.repoDir("flow")
	origin := filepath.ToSlash(root) + "/origin.git"
	o.run(root, "init", "-q", "--bare", "-b", "master", origin)
	o.run(root, "clone", "-q", origin, "work")
	b := &mergeBuilder{o: o, dir: filepath.Join(root, "work"), clock: mergeClockStart}
	for _, kv := range [][2]string{
		{"core.autocrlf", "false"},
		{"user.name", "oracle"},
		{"user.email", "oracle@example.com"},
		{"gitflow.branch.master", "master"},
		{"gitflow.branch.develop", "develop"},
		{"gitflow.prefix.feature", "feature/"},
		{"gitflow.prefix.release", "release/"},
		{"gitflow.prefix.hotfix", "hotfix/"},
		{"gitflow.prefix.support", "support/"},
		{"gitflow.prefix.versiontag", ""},
	} {
		o.run(b.dir, "config", kv[0], kv[1])
	}
	b.commit("base", map[string]string{"f": lines("f", 10)})
	b.git("push", "-q", "origin", "master")
	b.git("checkout", "-q", "-b", "develop")
	b.commit("develop work", map[string]string{"g": "develop\n"})
	b.git("push", "-q", "origin", "develop")
	return b
}

func flowOracleState(b *mergeBuilder) string {
	b.o.t.Helper()
	var out []string
	add := func(label, text string) { out = append(out, "== "+label+"\n"+text) }
	add("head", b.o.run(b.dir, "symbolic-ref", "HEAD"))
	add("local refs", b.o.run(b.dir, "for-each-ref", "--format=%(refname) %(objectname) %(objecttype)"))
	add("origin refs", b.o.run(b.dir, "ls-remote", "origin"))
	add("status", b.o.run(b.dir, "status", "--porcelain", "-uall"))
	tracking, _ := b.o.attempt(b.dir, "config", "--get-regexp", `^branch\.`)
	add("branch config", tracking)
	return strings.Join(out, "\n")
}

func fixedDateOracle(o *oracle) *oracle {
	stamp := strconv.FormatInt(flowOracleStamp, 10) + " +0000"
	return &oracle{t: o.t, home: o.home, env: append(slices.Clone(o.env), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)}
}

func TestOracleStartReleaseBranchesLikeSmartGit(t *testing.T) {
	o := newOracle(t)
	gitSide := flowOracleSide(t, o)
	ourSide := flowOracleSide(t, o)

	fixed := fixedDateOracle(o)
	fixed.run(gitSide.dir, "fetch", "--prune", "--force", "--recurse-submodules=no", "origin", "refs/heads/develop:refs/remotes/origin/develop")
	fixed.run(gitSide.dir, "branch", "--no-track", "release/v1.0.0", "refs/heads/develop")
	fixed.run(gitSide.dir, "checkout", "-q", "release/v1.0.0")
	if _, err := StartRelease(t.Context(), o.openRepo(ourSide.dir), "v1.0.0", StartReleaseOptions{Network: FlowNetwork{Remote: "origin"}}); err != nil {
		t.Fatalf("StartRelease returned error %v", err)
	}

	if got, want := flowOracleState(ourSide), flowOracleState(gitSide); got != want {
		t.Fatalf("repository differs from git: %s", sectionDiff(got, want))
	}
}

func TestOracleFinishReleaseMergesTagsAndPushesLikeSmartGit(t *testing.T) {
	o := newOracle(t)
	prepare := func(b *mergeBuilder) {
		b.git("branch", "--no-track", "release/v1.0.0", "refs/heads/develop")
		b.git("checkout", "-q", "release/v1.0.0")
		b.commit("bump version", map[string]string{"VERSION": "1.0.0\n"})
	}
	gitSide := flowOracleSide(t, o)
	prepare(gitSide)
	ourSide := flowOracleSide(t, o)
	prepare(ourSide)
	message := filepath.Join(t.TempDir(), "tag.txt")
	if err := os.WriteFile(message, []byte("Release v1.0.0\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	fixed := fixedDateOracle(o)
	for _, args := range [][]string{
		{"fetch", "--prune", "--force", "--recurse-submodules=no", "origin", "refs/heads/master:refs/remotes/origin/master"},
		{"fetch", "--prune", "--force", "--recurse-submodules=no", "origin", "refs/heads/develop:refs/remotes/origin/develop"},
		{"checkout", "-q", "--ignore-other-worktrees", "master"},
		{"merge", "-q", "--no-ff", "-m", "Finish v1.0.0", "release/v1.0.0"},
		{"tag", "-f", "-F", message, "v1.0.0", "refs/heads/master"},
		{"checkout", "-q", "--ignore-other-worktrees", "develop"},
		{"merge", "-q", "--no-ff", "-m", "Finish v1.0.0", "v1.0.0"},
		{"push", "--porcelain", "origin", "refs/heads/develop:refs/heads/develop"},
		{"push", "--porcelain", "origin", "refs/heads/master:refs/heads/master"},
		{"push", "--porcelain", "origin", "refs/tags/v1.0.0:refs/tags/v1.0.0"},
		{"branch", "-D", "release/v1.0.0"},
	} {
		fixed.run(gitSide.dir, args...)
	}
	result, err := FinishRelease(t.Context(), o.openRepo(ourSide.dir), "v1.0.0", FinishReleaseOptions{
		TagMessage:   "Release v1.0.0",
		Push:         true,
		DeleteBranch: true,
		When:         time.Unix(flowOracleStamp, 0).UTC(),
		Network:      FlowNetwork{Remote: "origin"},
	})
	if err != nil || !result.Finished() {
		t.Fatalf("FinishRelease = %+v, %v", result, err)
	}

	if got, want := flowOracleState(ourSide), flowOracleState(gitSide); got != want {
		t.Fatalf("repository differs from git: %s", sectionDiff(got, want))
	}
	o.run(ourSide.dir, "fsck", "--strict")
}
