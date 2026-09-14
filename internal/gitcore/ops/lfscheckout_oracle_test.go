//go:build oracle

package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var lfsCheckoutFiles = []string{"a.bin", "d.bin", "missing.bin", "sub/c.bin", "sub/e.bin"}

func lfsCheckoutSide(o *oracle, name, fetchExclude string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	if _, err := o.attempt(dir, "lfs", "install", "--local"); err != nil {
		o.t.Skipf("git-lfs is not available: %v", err)
	}
	o.write(dir, ".gitattributes", "*.bin filter=lfs -text\n")
	o.write(dir, "a.bin", "first\x00payload")
	o.write(dir, "sub/c.bin", "third\x00payload")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "lfs")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.write(dir, "a.bin", "second\x00payload")
	o.write(dir, "d.bin", "fourth\x00payload")
	o.write(dir, "sub/e.bin", "fifth\x00payload")
	o.write(dir, "missing.bin", "gone\x00payload")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.run(dir, "config", "lfs.fetchexclude", fetchExclude)
	return dir
}

func (o *oracle) lfsCheckoutState(dir string) string {
	o.t.Helper()
	out := o.indexState(dir) + o.run(dir, "status", "--porcelain=v2")
	for _, rel := range lfsCheckoutFiles {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			out += rel + " absent\n"
			continue
		}
		out += rel + "=" + string(data) + "\n"
	}
	return out
}

func TestOracleCheckoutSmudgesLocalLFSObjectsLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := lfsCheckoutSide(o, "git", "sub,missing.bin")
	ourSide := lfsCheckoutSide(o, "ours", "sub")
	sum := sha256.Sum256([]byte("gone\x00payload"))
	oid := hex.EncodeToString(sum[:])
	if err := os.Remove(filepath.Join(ourSide, ".git", "lfs", "objects", oid[:2], oid[2:4], oid)); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}

	o.run(gitSide, "checkout", "-q", "topic")
	var report CheckoutReport
	if err := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{Report: &report}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got, want := o.lfsCheckoutState(ourSide), o.lfsCheckoutState(gitSide); got != want {
		t.Fatalf("after switching to topic\nours:\n%s\ngit:\n%s", got, want)
	}
	reported := []string{}
	for _, pointer := range report.LFSPointers {
		reported = append(reported, pointer.Path)
	}
	if !slices.Equal(reported, []string{"missing.bin", "sub/e.bin"}) || report.LFSPointers[0].Reason != LFSObjectMissing || report.LFSPointers[1].Reason != LFSSmudgeSkipped {
		t.Fatalf("report = %+v", report)
	}

	for _, dir := range []string{gitSide, ourSide} {
		for _, rel := range lfsCheckoutFiles {
			o.ageFile(dir, rel)
		}
	}
	o.run(gitSide, "add", "--", "a.bin", "d.bin", "missing.bin", "sub")
	if err := Stage(t.Context(), o.openRepo(ourSide), []string{"a.bin", "d.bin", "missing.bin", "sub"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	if got, want := o.lfsCheckoutState(ourSide), o.lfsCheckoutState(gitSide); got != want {
		t.Fatalf("after staging\nours:\n%s\ngit:\n%s", got, want)
	}

	o.run(gitSide, "checkout", "-q", "main")
	if err := Switch(t.Context(), o.openRepo(ourSide), "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got, want := o.lfsCheckoutState(ourSide), o.lfsCheckoutState(gitSide); got != want {
		t.Fatalf("after switching back\nours:\n%s\ngit:\n%s", got, want)
	}
}
