package ops

import (
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

func TestSamePathFollowsTheCaseRulesOfThePlatform(t *testing.T) {
	restore := caseInsensitivePaths
	t.Cleanup(func() { caseInsensitivePaths = restore })
	upper, lower := filepath.Join(t.TempDir(), "A"), ""
	lower = filepath.Join(filepath.Dir(upper), "a")

	caseInsensitivePaths = false
	if samePath(upper, lower) {
		t.Fatal("paths that differ in case matched on a case-sensitive system")
	}
	caseInsensitivePaths = true
	if !samePath(upper, lower) {
		t.Fatal("paths that differ in case did not match on a case-insensitive system")
	}
}

func TestTheMainWorktreeIsFoundFromALinkedOne(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "linked")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "linked"}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}
	opened, err := repo.Open(target, r.openOptions())
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })

	list, err := ListWorktrees(opened)
	if err != nil {
		t.Fatalf("ListWorktrees returned error %v", err)
	}
	main, _ := mainWorktreePath(opened.CommonDir())
	if len(list) != 2 || !list[0].Main || list[0].Bare || !samePath(list[0].Path, main) || samePath(list[0].Path, target) {
		t.Fatalf("worktrees = %+v; want the main one at %s", list, main)
	}
}

func TestAMainWorktreeWithoutADotGitDirectoryIsBare(t *testing.T) {
	if path, bare := mainWorktreePath(filepath.Join(t.TempDir(), "repo.git")); !bare || filepath.Base(path) != "repo.git" {
		t.Fatalf("mainWorktreePath = %q, %v", path, bare)
	}
}
