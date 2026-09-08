//go:build oracle

package ops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

func porcelainWorktrees(text string) []map[string]string {
	var out []map[string]string
	current := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "" {
			if len(current) > 0 {
				out = append(out, current)
				current = map[string]string{}
			}
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		current[key] = value
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

func TestOurWorktreeIsTheOneGitReports(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("main")
	o.run(dir, "init", "--initial-branch=main")
	o.write(dir, "a.txt", "one\n")
	o.run(dir, "add", "a.txt")
	o.run(dir, "-c", "user.name=ann", "-c", "user.email=ann@example.com", "commit", "-m", "first")

	r, err := repo.Open(dir, o.options())
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	linked := filepath.Join(t.TempDir(), "feature")
	wt, err := AddWorktree(t.Context(), r, linked, AddWorktreeOptions{Branch: "feature"})
	if err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}

	reported := porcelainWorktrees(o.run(dir, "worktree", "list", "--porcelain"))
	if len(reported) != 2 {
		t.Fatalf("git reports %d worktrees, want the main one and ours: %v", len(reported), reported)
	}
	got := reported[1]
	if !samePath(got["worktree"], linked) {
		t.Fatalf("git reports the worktree at %q, want %q", got["worktree"], linked)
	}
	if got["HEAD"] != wt.Head.String() {
		t.Fatalf("git reports HEAD %q, want %q", got["HEAD"], wt.Head)
	}
	if got["branch"] != string(wt.Branch) {
		t.Fatalf("git reports branch %q, want %q", got["branch"], wt.Branch)
	}
	if status := o.run(linked, "status", "--porcelain"); status != "" {
		t.Fatalf("git status in our worktree = %q, want it clean", status)
	}
}

func TestGitPrunesAndLocksTheWorktreesWeWrite(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("main")
	o.run(dir, "init", "--initial-branch=main")
	o.write(dir, "a.txt", "one\n")
	o.run(dir, "add", "a.txt")
	o.run(dir, "-c", "user.name=ann", "-c", "user.email=ann@example.com", "commit", "-m", "first")

	r, err := repo.Open(dir, o.options())
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	linked := filepath.Join(t.TempDir(), "locked")
	if _, err := AddWorktree(t.Context(), r, linked, AddWorktreeOptions{Branch: "locked"}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}
	if err := LockWorktree(r, linked, "held by a test"); err != nil {
		t.Fatalf("LockWorktree returned error %v", err)
	}

	reported := porcelainWorktrees(o.run(dir, "worktree", "list", "--porcelain"))
	if _, locked := reported[1]["locked"]; !locked {
		t.Fatalf("git does not see our lock: %v", reported[1])
	}
	if out := o.run(dir, "worktree", "prune", "--dry-run"); out != "" {
		t.Fatalf("git would prune a healthy worktree: %q", out)
	}
}

func TestWeReadTheWorktreeGitCreated(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("main")
	o.run(dir, "init", "--initial-branch=main")
	o.write(dir, "a.txt", "one\n")
	o.run(dir, "add", "a.txt")
	o.run(dir, "-c", "user.name=ann", "-c", "user.email=ann@example.com", "commit", "-m", "first")
	linked := filepath.Join(t.TempDir(), "made-by-git")
	o.run(dir, "worktree", "add", "-b", "made", linked)

	r, err := repo.Open(dir, o.options())
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	list, err := ListWorktrees(r)
	if err != nil {
		t.Fatalf("ListWorktrees returned error %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("worktrees = %d, want the main one and the one git made", len(list))
	}
	if !samePath(list[1].Path, linked) || list[1].Branch != "refs/heads/made" {
		t.Fatalf("worktree = %+v, want %s on refs/heads/made", list[1], linked)
	}
	head := strings.TrimSpace(o.run(linked, "rev-parse", "HEAD"))
	if list[1].Head.String() != head {
		t.Fatalf("head = %v, want %s", list[1].Head, head)
	}
}
