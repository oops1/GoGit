//go:build oracle

package ops

import (
	"path/filepath"
	"strings"
	"testing"
)

func cloneOracleCommit(o *oracle, src, file string) {
	o.write(src, file, file+"\n")
	o.run(src, "add", ".")
	o.run(src, "commit", "-q", "-m", file)
}

func cloneOracleState(o *oracle, dir string) string {
	head, _ := o.attempt(dir, "symbolic-ref", "-q", "HEAD")
	var refsList []string
	for line := range strings.SplitSeq(o.run(dir, "for-each-ref", "--format=%(refname) %(objectname)"), "\n") {
		if line != "" {
			refsList = append(refsList, line)
		}
	}
	branches, _ := o.attempt(dir, "config", "--get-regexp", `^branch\.`)
	remoteHead, _ := o.attempt(dir, "symbolic-ref", "-q", "refs/remotes/origin/HEAD")
	return strings.Join([]string{
		"== head\n" + head,
		"== origin head\n" + remoteHead,
		"== commit\n" + o.run(dir, "rev-parse", "HEAD"),
		"== refs\n" + strings.Join(refsList, "\n"),
		"== branch config\n" + branches,
		"== fetch\n" + o.run(dir, "config", "--get-all", "remote.origin.fetch"),
	}, "\n")
}

func TestOracleCloneOfADetachedHeadGuessesTheBranchLikeGit(t *testing.T) {
	for _, c := range []struct {
		name   string
		single bool
		setup  func(o *oracle, src string)
	}{
		{"master comes before other branches at HEAD", false, func(o *oracle, src string) {
			o.run(src, "branch", "alpha")
			o.run(src, "branch", "master")
			cloneOracleCommit(o, src, "second.txt")
			o.run(src, "checkout", "-q", "--detach", "alpha")
		}},
		{"the first branch at HEAD", false, func(o *oracle, src string) {
			o.run(src, "branch", "zeta")
			o.run(src, "branch", "topic")
			cloneOracleCommit(o, src, "second.txt")
			o.run(src, "checkout", "-q", "--detach", "zeta")
		}},
		{"a single branch at HEAD", true, func(o *oracle, src string) {
			o.run(src, "branch", "topic")
			cloneOracleCommit(o, src, "second.txt")
			o.run(src, "checkout", "-q", "--detach", "topic")
		}},
		{"no branch at HEAD", false, func(o *oracle, src string) {
			cloneOracleCommit(o, src, "second.txt")
			o.run(src, "checkout", "-q", "--detach", "HEAD~1")
		}},
		{"a commit no branch contains", false, func(o *oracle, src string) {
			o.run(src, "checkout", "-q", "--detach")
			cloneOracleCommit(o, src, "detached.txt")
		}},
		{"a symbolic HEAD", false, func(o *oracle, src string) {
			o.run(src, "branch", "topic")
			cloneOracleCommit(o, src, "second.txt")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := newOracle(t)
			src := o.repoDir("src")
			newOracleRepo(o, src)
			cloneOracleCommit(o, src, "first.txt")
			c.setup(o, src)

			gitDest := filepath.Join(t.TempDir(), "git")
			args := []string{"clone", "-q"}
			if c.single {
				args = append(args, "--single-branch")
			}
			o.run("", append(args, src, gitDest)...)
			ourDest := filepath.Join(t.TempDir(), "ours")
			r, err := Clone(t.Context(), src, ourDest, CloneOptions{SingleBranch: c.single})
			if err != nil {
				t.Fatalf("Clone returned error %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatalf("Close returned error %v", err)
			}

			if got, want := cloneOracleState(o, ourDest), cloneOracleState(o, gitDest); got != want {
				t.Fatalf("clone differs from git: %s", sectionDiff(got, want))
			}
			o.run(ourDest, "fsck", "--strict")
		})
	}
}
