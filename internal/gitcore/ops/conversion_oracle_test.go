//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func (o *oracle) ageFile(dir, rel string) {
	o.t.Helper()
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(rel)), past, past); err != nil {
		o.t.Fatalf("Chtimes returned error %v", err)
	}
}

func (o *oracle) indexState(dir string, files ...string) string {
	o.t.Helper()
	out := o.run(dir, "status", "--porcelain") + o.run(dir, "symbolic-ref", "HEAD") + o.run(dir, "ls-files", "-s")
	for _, rel := range files {
		out += rel + "=" + o.read(dir, rel) + "\n"
	}
	return out
}

func crlfCommittedSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "a.txt", "one\r\ntwo\r\n")
	o.write(dir, "b.txt", "keep\r\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "crlf")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.write(dir, "a.txt", "one\r\ntwo\r\nthree\r\n")
	o.run(dir, "commit", "-q", "-am", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.run(dir, "config", "core.autocrlf", "true")
	o.ageFile(dir, "a.txt")
	return dir
}

func TestOracleSwitchOverCRLFCommittedWithoutAttributesLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := crlfCommittedSide(o, "git")
	ourSide := crlfCommittedSide(o, "ours")

	o.run(gitSide, "checkout", "-q", "topic")
	if err := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got, want := o.indexState(ourSide, "a.txt", "b.txt"), o.indexState(gitSide, "a.txt", "b.txt"); got != want {
		t.Fatalf("after the switch\nours:\n%q\ngit:\n%q", got, want)
	}
}

func TestOracleStageKeepsCRLFTheIndexHoldsLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := crlfCommittedSide(o, "git")
	ourSide := crlfCommittedSide(o, "ours")
	for _, dir := range []string{gitSide, ourSide} {
		o.write(dir, "a.txt", "one\r\ntwo\r\nnew\r\n")
		o.write(dir, "c.txt", "fresh\r\nfile\r\n")
		o.write(dir, "d.txt", "mixed\r\nold\rmac\n")
	}

	o.run(gitSide, "add", "a.txt", "c.txt", "d.txt")
	if err := Stage(t.Context(), o.openRepo(ourSide), []string{"a.txt", "c.txt", "d.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	if got, want := o.indexState(ourSide), o.indexState(gitSide); got != want {
		t.Fatalf("after staging\nours:\n%s\ngit:\n%s", got, want)
	}
}

func lfsSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	if _, err := o.attempt(dir, "lfs", "install", "--local"); err != nil {
		o.t.Skipf("git-lfs is not available: %v", err)
	}
	o.write(dir, ".gitattributes", "*.bin filter=lfs -text\n")
	o.write(dir, "a.bin", "first\x00payload")
	o.write(dir, "b.bin", "stays\x00payload")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "lfs")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.write(dir, "a.bin", "second\x00payload")
	o.run(dir, "commit", "-q", "-am", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.ageFile(dir, "a.bin")
	o.ageFile(dir, "b.bin")
	return dir
}

func TestOracleSwitchAndStageOverLFSFilesLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := lfsSide(o, "git")
	ourSide := lfsSide(o, "ours")

	o.run(gitSide, "checkout", "-q", "topic")
	if err := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got, want := o.indexState(ourSide), o.indexState(gitSide); got != want {
		t.Fatalf("after the switch\nours:\n%s\ngit:\n%s", got, want)
	}

	for _, dir := range []string{gitSide, ourSide} {
		o.ageFile(dir, "a.bin")
		o.ageFile(dir, "b.bin")
	}
	o.run(gitSide, "add", "a.bin", "b.bin")
	if err := Stage(t.Context(), o.openRepo(ourSide), []string{"a.bin", "b.bin"}, StageOptions{}); err != nil {
		t.Fatalf("Stage of unchanged LFS files returned error %v", err)
	}
	if got, want := o.indexState(ourSide), o.indexState(gitSide); got != want {
		t.Fatalf("after staging\nours:\n%s\ngit:\n%s", got, want)
	}
}
