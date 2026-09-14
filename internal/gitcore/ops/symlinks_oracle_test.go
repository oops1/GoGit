//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPlainSymlinkOracle(t *testing.T) (*oracle, string) {
	t.Helper()
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.symlinks", "false")
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "link", "target.txt")
	id := strings.TrimSpace(o.run(dir, "hash-object", "-w", "link"))
	o.run(dir, "update-index", "--add", "--cacheinfo", "120000,"+id+",link")
	o.run(dir, "commit", "-q", "-m", "symlink")
	return o, dir
}

func requireRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat returned error %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s has mode %v, want a regular file", path, info.Mode())
	}
}

func TestOracleDiscardChecksOutASymlinkLikeGitWithoutSymlinkSupport(t *testing.T) {
	o, dir := newPlainSymlinkOracle(t)
	o.remove(dir, "link")
	o.run(dir, "checkout", "--", "link")
	requireRegularFile(t, filepath.Join(dir, "link"))
	want := o.read(dir, "link")
	o.remove(dir, "link")

	if err := Discard(t.Context(), o.openRepo(dir), []string{"link"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	requireRegularFile(t, filepath.Join(dir, "link"))
	if got := o.read(dir, "link"); got != want {
		t.Fatalf("link holds %q, git checks out %q", got, want)
	}
	if status := o.run(dir, "status", "--porcelain"); status != "" {
		t.Fatalf("git status reports %q after Discard", status)
	}
}

func TestOracleStageKeepsTheSymlinkModeLikeGitWithoutSymlinkSupport(t *testing.T) {
	o, dir := newPlainSymlinkOracle(t)
	o.write(dir, "link", "elsewhere.txt")

	if err := Stage(t.Context(), o.openRepo(dir), []string{"link"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	ours := o.run(dir, "ls-files", "-s", "link")
	o.run(dir, "reset", "-q")
	o.run(dir, "add", "link")
	if want := o.run(dir, "ls-files", "-s", "link"); ours != want {
		t.Fatalf("Stage recorded %q, git add records %q", ours, want)
	}
	if !strings.HasPrefix(ours, "120000 ") {
		t.Fatalf("Stage recorded %q, want mode 120000", ours)
	}
}
