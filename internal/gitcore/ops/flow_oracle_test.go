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

type flowOracleCase struct {
	kind    string
	branch  string
	name    string
	base    string
	tagged  bool
	message string
}

var flowOracleCases = []flowOracleCase{
	{kind: FlowKindFeature, branch: "feature/login", name: "login", base: "develop", message: "Add login"},
	{kind: FlowKindRelease, branch: "release/v1.0.0", name: "v1.0.0", base: "develop", tagged: true, message: "Release v1.0.0"},
	{kind: FlowKindHotfix, branch: "hotfix/v1.0.1", name: "v1.0.1", base: "master", tagged: true, message: "Hotfix v1.0.1"},
}

func TestOracleStartFlowBranchesLikeSmartGit(t *testing.T) {
	for _, c := range flowOracleCases {
		t.Run(c.kind, func(t *testing.T) {
			o := newOracle(t)
			gitSide := flowOracleSide(t, o)
			ourSide := flowOracleSide(t, o)

			fixed := fixedDateOracle(o)
			fixed.run(gitSide.dir, "fetch", "--prune", "--force", "--recurse-submodules=no", "origin", "refs/heads/"+c.base+":refs/remotes/origin/"+c.base)
			fixed.run(gitSide.dir, "branch", "--no-track", c.branch, "refs/heads/"+c.base)
			fixed.run(gitSide.dir, "checkout", "-q", c.branch)
			if _, err := StartFlow(t.Context(), o.openRepo(ourSide.dir), c.kind, c.name, StartFlowOptions{Network: FlowNetwork{Remote: "origin"}}); err != nil {
				t.Fatalf("StartFlow returned error %v", err)
			}

			if got, want := flowOracleState(ourSide), flowOracleState(gitSide); got != want {
				t.Fatalf("repository differs from git: %s", sectionDiff(got, want))
			}
		})
	}
}

func flowOracleFinishCommands(c flowOracleCase, message string) [][]string {
	fetch := func(branch string) []string {
		return []string{"fetch", "--prune", "--force", "--recurse-submodules=no", "origin", "refs/heads/" + branch + ":refs/remotes/origin/" + branch}
	}
	push := func(ref string) []string { return []string{"push", "--porcelain", "origin", ref + ":" + ref} }
	finish := "Finish " + c.name
	if !c.tagged {
		return [][]string{
			fetch("develop"),
			{"checkout", "-q", "--ignore-other-worktrees", "develop"},
			{"merge", "-q", "--no-ff", "-m", c.message, c.branch},
			push("refs/heads/develop"),
			{"branch", "-D", c.branch},
		}
	}
	return [][]string{
		fetch("master"),
		fetch("develop"),
		{"checkout", "-q", "--ignore-other-worktrees", "master"},
		{"merge", "-q", "--no-ff", "-m", finish, c.branch},
		{"tag", "-f", "-F", message, c.name, "refs/heads/master"},
		{"checkout", "-q", "--ignore-other-worktrees", "develop"},
		{"merge", "-q", "--no-ff", "-m", finish, c.name},
		push("refs/heads/develop"),
		push("refs/heads/master"),
		push("refs/tags/" + c.name),
		{"branch", "-D", c.branch},
	}
}

func TestOracleFinishFlowMergesTagsAndPushesLikeSmartGit(t *testing.T) {
	for _, c := range flowOracleCases {
		t.Run(c.kind, func(t *testing.T) {
			o := newOracle(t)
			prepare := func(b *mergeBuilder) {
				b.git("branch", "--no-track", c.branch, "refs/heads/"+c.base)
				b.git("checkout", "-q", c.branch)
				b.commit("work on "+c.name, map[string]string{"VERSION": c.name + "\n"})
			}
			gitSide := flowOracleSide(t, o)
			prepare(gitSide)
			ourSide := flowOracleSide(t, o)
			prepare(ourSide)
			message := filepath.Join(t.TempDir(), "tag.txt")
			if err := os.WriteFile(message, []byte(c.message+"\n"), 0o666); err != nil {
				t.Fatalf("WriteFile returned error %v", err)
			}

			fixed := fixedDateOracle(o)
			for _, args := range flowOracleFinishCommands(c, message) {
				fixed.run(gitSide.dir, args...)
			}
			result, err := FinishFlow(t.Context(), o.openRepo(ourSide.dir), c.kind, c.name, FinishFlowOptions{
				Message:      c.message,
				Push:         true,
				DeleteBranch: true,
				When:         time.Unix(flowOracleStamp, 0).UTC(),
				Network:      FlowNetwork{Remote: "origin"},
			})
			if err != nil || !result.Finished() {
				t.Fatalf("FinishFlow = %+v, %v", result, err)
			}

			if got, want := flowOracleState(ourSide), flowOracleState(gitSide); got != want {
				t.Fatalf("repository differs from git: %s", sectionDiff(got, want))
			}
			o.run(ourSide.dir, "fsck", "--strict")
		})
	}
}
