//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	firstGitlinkID  = "1111111111111111111111111111111111111111"
	secondGitlinkID = "2222222222222222222222222222222222222222"
)

func (o *oracle) commitGitlink(dir, rel, id, message string) {
	o.t.Helper()
	o.run(dir, "update-index", "--add", "--cacheinfo", "160000,"+id+","+rel)
	o.run(dir, "commit", "-q", "-m", message)
}

func (o *oracle) gitlinkCheckoutState(dir, rel string) string {
	o.t.Helper()
	info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	directory := "missing"
	if err == nil && info.IsDir() {
		directory = "directory"
	}
	return "== status\n" + o.run(dir, "status", "--porcelain=v2") +
		"== index\n" + o.run(dir, "ls-files", "-s") +
		"== " + rel + "\n" + directory
}

func TestOracleCloneOfASuperprojectChecksOutTheSubmoduleDirectoryLikeGit(t *testing.T) {
	o := newOracle(t)
	src := o.repoDir("src")
	newOracleRepo(o, src)
	o.write(src, "a.txt", "hello\n")
	o.run(src, "add", ".")
	o.commitGitlink(src, "libs/sub", firstGitlinkID, "superproject")

	parent := t.TempDir()
	o.run(parent, "clone", "-q", src, "git")
	ours := filepath.Join(parent, "ours")
	r, err := Clone(t.Context(), src, ours, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	got, want := o.gitlinkCheckoutState(ours, "libs/sub"), o.gitlinkCheckoutState(filepath.Join(parent, "git"), "libs/sub")
	if got != want {
		t.Fatalf("after the clone\nours:\n%s\ngit:\n%s", got, want)
	}
}

func movedGitlinkSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.commitGitlink(dir, "sub", firstGitlinkID, "one")
	o.run(dir, "checkout", "-q", "-b", "two")
	o.commitGitlink(dir, "sub", secondGitlinkID, "two")
	o.run(dir, "checkout", "-q", "main")
	if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
		o.t.Fatal(err)
	}
	return dir
}

func TestOracleSwitchMovesASubmoduleWhoseDirectoryIsMissingLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := movedGitlinkSide(o, "git")
	ourSide := movedGitlinkSide(o, "ours")

	o.run(gitSide, "checkout", "-q", "two")
	if err := Switch(t.Context(), o.openRepo(ourSide), "two", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}

	if got, want := o.gitlinkCheckoutState(ourSide, "sub"), o.gitlinkCheckoutState(gitSide, "sub"); got != want {
		t.Fatalf("after the switch\nours:\n%s\ngit:\n%s", got, want)
	}
}
