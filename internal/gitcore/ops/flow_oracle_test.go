//go:build oracle

package ops

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const flowOracleStamp = 1700050000

func flowOracleSide(t *testing.T, o *oracle, tracked bool) *mergeBuilder {
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
		{"gitflow.origin", "origin"},
	} {
		o.run(b.dir, "config", kv[0], kv[1])
	}
	b.commit("base", map[string]string{"f": lines("f", 10)})
	b.git("tag", "-a", "-m", "v0.9", "v0.9", "master")
	b.git("checkout", "-q", "-b", "develop")
	b.commit("develop work", map[string]string{"g": "develop\n"})
	if tracked {
		b.git("push", "-q", "origin", "master", "develop")
	}
	return b
}

func flowOracleState(b *mergeBuilder) string {
	b.o.t.Helper()
	var out []string
	add := func(label, text string) { out = append(out, "== "+label+"\n"+text) }
	add("head", b.o.run(b.dir, "symbolic-ref", "HEAD"))
	add("local refs", b.o.run(b.dir, "for-each-ref", "--format=%(refname) %(objectname) %(objecttype)"))
	origin, _ := b.o.attempt(b.dir, "ls-remote", "origin")
	add("origin refs", origin)
	add("status", b.o.run(b.dir, "status", "--porcelain", "-uall"))
	tracking, _ := b.o.attempt(b.dir, "config", "--get-regexp", `^branch\.`)
	add("branch config", tracking)
	return strings.Join(out, "\n")
}

func fixedDateOracle(o *oracle) *oracle {
	stamp := strconv.FormatInt(flowOracleStamp, 10) + " +0000"
	return &oracle{t: o.t, home: o.home, env: append(slices.Clone(o.env), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)}
}

func sameAsGit(t *testing.T, ours, git *mergeBuilder) {
	t.Helper()
	if got, want := flowOracleState(ours), flowOracleState(git); got != want {
		t.Fatalf("repository differs from git: %s", sectionDiff(got, want))
	}
}

func flowFetchArgs(branch string) []string {
	return []string{"fetch", "--prune", "--force", "--recurse-submodules=no", "origin", "refs/heads/" + branch + ":refs/remotes/origin/" + branch}
}

var flowOrigin = FlowNetwork{Remote: "origin"}

func TestOracleStartFlowBranchesLikeSmartGit(t *testing.T) {
	for _, c := range []struct {
		label, kind, name, base, start string
		tracked                        bool
	}{
		{"feature", FlowKindFeature, "Feature-1", "", "refs/heads/develop", false},
		{"release with a tracked develop", FlowKindRelease, "v1.0.0", "", "refs/heads/develop", true},
		{"hotfix", FlowKindHotfix, "Hotfix-1", "", "refs/heads/master", false},
		{"feature from a chosen base", FlowKindFeature, "fix", "master", "refs/heads/master", true},
		{"support from a tag", FlowKindSupport, "0.9.x", "v0.9", "refs/tags/v0.9", true},
	} {
		t.Run(c.label, func(t *testing.T) {
			o := newOracle(t)
			gitSide := flowOracleSide(t, o, c.tracked)
			ourSide := flowOracleSide(t, o, c.tracked)
			branch := c.kind + "/" + c.name

			fixed := fixedDateOracle(o)
			if c.tracked && strings.HasPrefix(c.start, "refs/heads/") {
				fixed.run(gitSide.dir, flowFetchArgs(strings.TrimPrefix(c.start, "refs/heads/"))...)
			}
			fixed.run(gitSide.dir, "branch", "--no-track", branch, c.start)
			fixed.run(gitSide.dir, "checkout", "-q", branch)
			if _, err := StartFlow(t.Context(), o.openRepo(ourSide.dir), c.kind, c.name, StartFlowOptions{Base: c.base, Network: flowOrigin}); err != nil {
				t.Fatalf("StartFlow returned error %v", err)
			}

			sameAsGit(t, ourSide, gitSide)
		})
	}
}

type flowFinishOracle struct {
	label    string
	kind     string
	name     string
	tracked  bool
	pushed   bool
	moreBase bool
	oldTag   bool
	opts     FinishFlowOptions
	git      func(message string) [][]string
}

func flowFinishOracles() []flowFinishOracle {
	checkout := func(branch string) []string { return []string{"checkout", "-q", "--ignore-other-worktrees", branch} }
	push := func(ref string) []string { return []string{"push", "--porcelain", "origin", ref} }
	return []flowFinishOracle{
		{label: "feature by a merge commit", kind: FlowKindFeature, name: "Feature-1", opts: FinishFlowOptions{DeleteBranch: true},
			git: func(string) [][]string {
				return [][]string{
					checkout("develop"),
					{"merge", "-q", "--no-ff", "-m", "Finish Feature-1", "feature/Feature-1"},
					{"checkout", "-q", "develop"},
					{"branch", "-D", "feature/Feature-1"},
				}
			}},
		{label: "feature by a squash", kind: FlowKindFeature, name: "Feature-1", opts: FinishFlowOptions{Integration: FlowSquash, Message: "Add feature one", DeleteBranch: true},
			git: func(string) [][]string {
				return [][]string{
					checkout("develop"),
					{"merge", "-q", "--squash", "feature/Feature-1"},
					{"commit", "-q", "-m", "Add feature one"},
					{"branch", "-D", "feature/Feature-1"},
				}
			}},
		{label: "feature by a rebase", kind: FlowKindFeature, name: "Feature-1", moreBase: true, opts: FinishFlowOptions{Integration: FlowRebase, DeleteBranch: true},
			git: func(string) [][]string {
				return [][]string{
					{"checkout", "-q", "feature/Feature-1"},
					{"rebase", "-q", "develop"},
					checkout("develop"),
					{"merge", "-q", "--ff-only", "feature/Feature-1"},
					{"branch", "-D", "feature/Feature-1"},
				}
			}},
		{label: "feature that was pushed", kind: FlowKindFeature, name: "Feature-1", tracked: true, pushed: true, opts: FinishFlowOptions{Fetch: true, Push: true, DeleteBranch: true},
			git: func(string) [][]string {
				return [][]string{
					flowFetchArgs("develop"),
					checkout("develop"),
					{"merge", "-q", "--no-ff", "-m", "Finish Feature-1", "feature/Feature-1"},
					push(":refs/heads/feature/Feature-1"),
					{"branch", "-D", "feature/Feature-1"},
				}
			}},
		{label: "release without remote branches", kind: FlowKindRelease, name: "Release-1", opts: FinishFlowOptions{DeleteBranch: true},
			git: func(message string) [][]string {
				return [][]string{
					checkout("master"),
					{"merge", "-q", "--no-ff", "-m", "Finish Release-1", "release/Release-1"},
					{"tag", "-f", "-F", message, "Release-1", "refs/heads/master"},
					checkout("develop"),
					{"merge", "-q", "--no-ff", "-m", "Finish Release-1", "Release-1"},
					{"branch", "-D", "release/Release-1"},
				}
			}},
		{label: "release fetched and pushed", kind: FlowKindRelease, name: "v1.0.0", tracked: true, pushed: true, opts: FinishFlowOptions{Message: "Release v1.0.0", Fetch: true, Push: true, DeleteBranch: true},
			git: func(message string) [][]string {
				return [][]string{
					flowFetchArgs("master"),
					flowFetchArgs("develop"),
					checkout("master"),
					{"merge", "-q", "--no-ff", "-m", "Release v1.0.0", "release/v1.0.0"},
					{"tag", "-f", "-F", message, "v1.0.0", "refs/heads/master"},
					checkout("develop"),
					{"merge", "-q", "--no-ff", "-m", "Release v1.0.0", "v1.0.0"},
					push("refs/heads/develop:refs/heads/develop"),
					push("refs/heads/master:refs/heads/master"),
					push("refs/tags/v1.0.0:refs/tags/v1.0.0"),
					push(":refs/heads/release/v1.0.0"),
					{"branch", "-D", "release/v1.0.0"},
				}
			}},
		{label: "hotfix without remote branches", kind: FlowKindHotfix, name: "Hotfix-1", opts: FinishFlowOptions{DeleteBranch: true},
			git: func(message string) [][]string {
				return [][]string{
					checkout("master"),
					{"merge", "-q", "--no-ff", "-m", "Finish Hotfix-1", "hotfix/Hotfix-1"},
					{"tag", "-f", "-F", message, "Hotfix-1", "refs/heads/master"},
					checkout("develop"),
					{"merge", "-q", "--no-ff", "--no-edit", "Hotfix-1"},
					{"branch", "-D", "hotfix/Hotfix-1"},
				}
			}},
		{label: "hotfix without a tag and develop", kind: FlowKindHotfix, name: "Hotfix-1", opts: FinishFlowOptions{SkipTag: true, SkipDevelop: true},
			git: func(string) [][]string {
				return [][]string{
					checkout("master"),
					{"merge", "-q", "--no-ff", "-m", "Finish Hotfix-1", "hotfix/Hotfix-1"},
				}
			}},
		{label: "release over a tag that already exists", kind: FlowKindRelease, name: "Release-1", oldTag: true, opts: FinishFlowOptions{DeleteBranch: true},
			git: func(string) [][]string {
				return [][]string{
					checkout("master"),
					{"merge", "-q", "--no-ff", "-m", "Finish Release-1", "release/Release-1"},
					checkout("develop"),
					{"merge", "-q", "--no-ff", "-m", "Finish Release-1", "release/Release-1"},
					{"branch", "-D", "release/Release-1"},
				}
			}},
		{label: "hotfix pushed over a tag that already exists", kind: FlowKindHotfix, name: "Hotfix-1", tracked: true, pushed: true, oldTag: true, opts: FinishFlowOptions{Fetch: true, Push: true, DeleteBranch: true},
			git: func(string) [][]string {
				return [][]string{
					flowFetchArgs("master"),
					flowFetchArgs("develop"),
					checkout("master"),
					{"merge", "-q", "--no-ff", "-m", "Finish Hotfix-1", "hotfix/Hotfix-1"},
					checkout("develop"),
					{"merge", "-q", "--no-ff", "--no-edit", "hotfix/Hotfix-1"},
					push("refs/heads/develop:refs/heads/develop"),
					push("refs/heads/master:refs/heads/master"),
					push(":refs/heads/hotfix/Hotfix-1"),
					{"branch", "-D", "hotfix/Hotfix-1"},
				}
			}},
	}
}

func TestOracleFinishFlowFollowsTheSmartGitLog(t *testing.T) {
	for _, c := range flowFinishOracles() {
		t.Run(c.label, func(t *testing.T) {
			o := newOracle(t)
			base := "refs/heads/develop"
			if c.kind == FlowKindHotfix {
				base = "refs/heads/master"
			}
			branch := c.kind + "/" + c.name
			prepare := func(b *mergeBuilder) {
				if c.oldTag {
					b.git("tag", "-a", "-m", "earlier "+c.name, c.name, "master")
				}
				b.git("branch", "--no-track", branch, base)
				b.git("checkout", "-q", branch)
				b.commit("work on "+c.name, map[string]string{"VERSION": c.name + "\n"})
				if c.pushed {
					b.git("push", "-q", "origin", branch)
				}
				if c.moreBase {
					b.git("checkout", "-q", "develop")
					b.commit("more develop", map[string]string{"h": "more\n"})
					b.git("checkout", "-q", branch)
				}
			}
			gitSide := flowOracleSide(t, o, c.tracked)
			prepare(gitSide)
			ourSide := flowOracleSide(t, o, c.tracked)
			prepare(ourSide)
			message := filepath.Join(t.TempDir(), "tag.txt")
			if err := os.WriteFile(message, []byte(cmp.Or(c.opts.Message, "Finish "+c.name)+"\n"), 0o666); err != nil {
				t.Fatalf("WriteFile returned error %v", err)
			}

			fixed := fixedDateOracle(o)
			for _, args := range c.git(message) {
				fixed.run(gitSide.dir, args...)
			}
			opts := c.opts
			opts.When, opts.Network = time.Unix(flowOracleStamp, 0).UTC(), flowOrigin
			result, err := FinishFlow(t.Context(), o.openRepo(ourSide.dir), c.kind, c.name, opts)
			if err != nil || !result.Finished() {
				t.Fatalf("FinishFlow = %+v, %v", result, err)
			}

			sameAsGit(t, ourSide, gitSide)
			o.run(ourSide.dir, "fsck", "--strict")
		})
	}
}

func flowConfigureSide(t *testing.T, o *oracle, extra ...string) *mergeBuilder {
	t.Helper()
	root := o.repoDir("configure")
	o.run(root, "init", "-q", "-b", "main", "work")
	b := &mergeBuilder{o: o, dir: filepath.Join(root, "work"), clock: mergeClockStart}
	for _, kv := range [][2]string{{"core.autocrlf", "false"}, {"user.name", "oracle"}, {"user.email", "oracle@example.com"}} {
		o.run(b.dir, "config", kv[0], kv[1])
	}
	b.commit("base", map[string]string{"f": "base\n"})
	for _, branch := range extra {
		b.git("branch", branch)
	}
	return b
}

func TestOracleConfigureFlowCreatesTheBranchesLikeSmartGit(t *testing.T) {
	full := DefaultFlowConfig()
	for _, c := range []struct {
		label  string
		cfg    FlowConfig
		exists []string
		git    []string
	}{
		{"light", DefaultLightFlowConfig(), nil, []string{"branch", "--no-track", "master"}},
		{"full with master", full, []string{"master"}, []string{"branch", "--no-track", "develop"}},
	} {
		t.Run(c.label, func(t *testing.T) {
			o := newOracle(t)
			gitSide := flowConfigureSide(t, o, c.exists...)
			ourSide := flowConfigureSide(t, o, c.exists...)

			fixedDateOracle(o).run(gitSide.dir, c.git...)
			if _, err := ConfigureFlow(t.Context(), o.openRepo(ourSide.dir), c.cfg); err != nil {
				t.Fatalf("ConfigureFlow returned error %v", err)
			}

			sameAsGit(t, ourSide, gitSide)
		})
	}
}

func TestOracleFinishFlowStopsOnAnUncleanDirectoryRenameLikeGit(t *testing.T) {
	o := newOracle(t)
	prepare := func(b *mergeBuilder) {
		b.commit("library", map[string]string{"lib/a.go": lines("a", 10), "lib/b.go": lines("b", 10)})
		b.git("branch", "--no-track", "feature/Feature-1", "refs/heads/develop")
		b.git("checkout", "-q", "feature/Feature-1")
		b.commit("feature work", map[string]string{"lib/new.go": "new\n"})
		b.git("checkout", "-q", "develop")
		b.commit("split the library", map[string]string{"lib/a.go": "", "lib/b.go": "", "x/a.go": lines("a", 10), "y/b.go": lines("b", 10)})
		b.git("checkout", "-q", "feature/Feature-1")
	}
	gitSide := flowOracleSide(t, o, false)
	prepare(gitSide)
	ourSide := flowOracleSide(t, o, false)
	prepare(ourSide)

	fixed := fixedDateOracle(o)
	fixed.run(gitSide.dir, "checkout", "-q", "--ignore-other-worktrees", "develop")
	if _, err := fixed.attempt(gitSide.dir, "merge", "-q", "--no-ff", "-m", "Finish Feature-1", "feature/Feature-1"); err == nil {
		t.Fatal("git merged the feature cleanly")
	}
	result, err := FinishFlow(t.Context(), o.openRepo(ourSide.dir), FlowKindFeature, "Feature-1", FinishFlowOptions{DeleteBranch: true, When: time.Unix(flowOracleStamp, 0).UTC()})
	if err != nil || result.Finished() || result.Stopped != FlowStepMergeDevelop {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	sameAsGit(t, ourSide, gitSide)
	if got, want := o.read(ourSide.dir, ".git/MERGE_MSG"), o.read(gitSide.dir, ".git/MERGE_MSG"); got != want {
		t.Fatalf("MERGE_MSG = %q, git wrote %q", got, want)
	}
	ourHead, _ := o.attempt(ourSide.dir, "rev-parse", "MERGE_HEAD")
	gitHead, _ := o.attempt(gitSide.dir, "rev-parse", "MERGE_HEAD")
	if ourHead != gitHead || gitHead == "" {
		t.Fatalf("MERGE_HEAD = %q, git %q", ourHead, gitHead)
	}
}

func TestOracleIntegrateDevelopMergesOrRebasesLikeGit(t *testing.T) {
	for _, c := range []struct {
		label  string
		rebase bool
		git    []string
	}{
		{"merge", false, []string{"merge", "-q", "--no-edit", "develop"}},
		{"rebase", true, []string{"rebase", "-q", "develop"}},
	} {
		t.Run(c.label, func(t *testing.T) {
			o := newOracle(t)
			prepare := func(b *mergeBuilder) {
				b.git("branch", "--no-track", "feature/Feature-1", "refs/heads/develop")
				b.git("checkout", "-q", "feature/Feature-1")
				b.commit("feature work", map[string]string{"F1.txt": "feature\n"})
				b.git("checkout", "-q", "develop")
				b.commit("more develop", map[string]string{"h": "more\n"})
			}
			gitSide := flowOracleSide(t, o, false)
			prepare(gitSide)
			ourSide := flowOracleSide(t, o, false)
			prepare(ourSide)

			fixed := fixedDateOracle(o)
			fixed.run(gitSide.dir, "checkout", "-q", "feature/Feature-1")
			fixed.run(gitSide.dir, c.git...)
			integrated, err := IntegrateDevelop(t.Context(), o.openRepo(ourSide.dir), "Feature-1", IntegrateDevelopOptions{Rebase: c.rebase, When: time.Unix(flowOracleStamp, 0).UTC()})
			if err != nil || !integrated.Clean() {
				t.Fatalf("IntegrateDevelop = %+v, %v", integrated, err)
			}

			sameAsGit(t, ourSide, gitSide)
		})
	}
}
