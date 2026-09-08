package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func worktreePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestListWorktreesReportsTheMainWorktreeAlone(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	head := r.commitAll("first")

	list, err := ListWorktrees(r.repo)
	if err != nil {
		t.Fatalf("ListWorktrees returned error %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("worktrees = %d, want only the main one", len(list))
	}
	main := list[0]
	if !main.Main || main.Path != r.dir {
		t.Fatalf("main worktree = %+v, want the repository directory", main)
	}
	if main.Head != head || main.Branch != refs.BranchName("main") {
		t.Fatalf("main worktree head = %v/%v, want %v/main", main.Head, main.Branch, head)
	}
	if main.Detached {
		t.Fatal("a repository on a branch must not report a detached head")
	}
}

func TestAddWorktreeCreatesTheLinkGitReads(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	head := r.commitAll("first")
	target := worktreePath(t, "feature")

	wt, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "feature"})
	if err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}
	if wt.Head != head || wt.Branch != refs.BranchName("feature") {
		t.Fatalf("worktree = %+v, want it on the new branch at %v", wt, head)
	}

	admin := filepath.Join(r.repo.CommonDir(), worktreesDir, wt.ID)
	if got := readTrimmedFile(t, filepath.Join(admin, worktreeCommonDir)); got != commonDirTarget {
		t.Fatalf("commondir = %q, want %q", got, commonDirTarget)
	}
	if got := readTrimmedFile(t, filepath.Join(admin, worktreeGitDir)); got != filepath.ToSlash(filepath.Join(target, dotGitName)) {
		t.Fatalf("gitdir = %q, want the .git file of the worktree", got)
	}
	if got := readTrimmedFile(t, filepath.Join(admin, worktreeHeadFile)); got != "ref: refs/heads/feature" {
		t.Fatalf("HEAD = %q, want the new branch", got)
	}
	if got := readTrimmedFile(t, filepath.Join(target, dotGitName)); got != gitFileMarker+filepath.ToSlash(admin) {
		t.Fatalf(".git file = %q, want it to point at the admin directory", got)
	}
	if content, err := os.ReadFile(filepath.Join(target, "a.txt")); err != nil || string(content) != "one" {
		t.Fatalf("checked out file = %q, %v", content, err)
	}
}

func TestAddedWorktreeOpensAsARepositoryOfItsOwn(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	head := r.commitAll("first")
	target := worktreePath(t, "linked")

	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "linked"}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}

	opened, err := repo.Open(target, r.openOptions())
	if err != nil {
		t.Fatalf("repo.Open on the new worktree returned error %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	if !opened.IsWorktree() {
		t.Fatal("the new directory must open as a linked worktree")
	}
	if !samePath(opened.CommonDir(), r.repo.CommonDir()) {
		t.Fatalf("common dir = %q, want %q", opened.CommonDir(), r.repo.CommonDir())
	}
	store, err := refs.Open(refs.Options{GitDir: opened.GitDir(), CommonDir: opened.CommonDir()})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	branch, commit, err := currentHeadState(store)
	if err != nil {
		t.Fatalf("currentHeadState returned error %v", err)
	}
	if branch != refs.BranchName("linked") || commit != head {
		t.Fatalf("worktree head = %v/%v, want linked/%v", branch, commit, head)
	}
}

func TestListWorktreesReportsTheLinkedOne(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	head := r.commitAll("first")
	target := worktreePath(t, "second")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "second"}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}

	list, err := ListWorktrees(r.repo)
	if err != nil {
		t.Fatalf("ListWorktrees returned error %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("worktrees = %d, want the main one and the linked one", len(list))
	}
	linked := list[1]
	if linked.Main || linked.Path != target || linked.Head != head {
		t.Fatalf("linked worktree = %+v", linked)
	}
	if linked.Branch != refs.BranchName("second") || linked.Locked || linked.Prunable {
		t.Fatalf("linked worktree state = %+v", linked)
	}
}

func TestAddWorktreeDetachesWhenAsked(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	head := r.commitAll("first")

	wt, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "detached"), AddWorktreeOptions{Detach: true})
	if err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}
	if !wt.Detached || wt.Branch != "" || wt.Head != head {
		t.Fatalf("worktree = %+v, want a detached head at %v", wt, head)
	}
	admin := filepath.Join(r.repo.CommonDir(), worktreesDir, wt.ID)
	if got := readTrimmedFile(t, filepath.Join(admin, worktreeHeadFile)); got != head.String() {
		t.Fatalf("HEAD = %q, want the commit itself", got)
	}
}

func TestAddWorktreeRefusesABranchThatIsAlreadyCheckedOut(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "again"), AddWorktreeOptions{StartPoint: "main"})

	if !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("err = %v, want ErrBranchCheckedOut", err)
	}
}

func TestAddWorktreeRefusesADirectoryThatIsNotEmpty(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "busy")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Detach: true})

	if !errors.Is(err, ErrWorktreeTargetBusy) {
		t.Fatalf("err = %v, want ErrWorktreeTargetBusy", err)
	}
}

func TestAddWorktreeRefusesAPathThatIsAlreadyAWorktree(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "taken")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "taken"}); err != nil {
		t.Fatal(err)
	}

	_, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Detach: true})

	if !errors.Is(err, ErrWorktreeExists) {
		t.Fatalf("err = %v, want ErrWorktreeExists", err)
	}
}

func TestSecondWorktreeOfTheSameNameGetsItsOwnAdminDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	first, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "same"), AddWorktreeOptions{Branch: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "same"), AddWorktreeOptions{Branch: "two"})
	if err != nil {
		t.Fatal(err)
	}

	if first.ID == second.ID {
		t.Fatalf("both worktrees claim the administrative directory %q", first.ID)
	}
}

func TestRemoveWorktreeTakesTheDirectoryAndTheAdministrativeCopy(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "gone")
	wt, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "gone"})
	if err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(t.Context(), r.repo, target, false); err != nil {
		t.Fatalf("RemoveWorktree returned error %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("worktree directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.repo.CommonDir(), worktreesDir, wt.ID)); !os.IsNotExist(err) {
		t.Fatalf("administrative directory still exists: %v", err)
	}
}

func TestRemoveWorktreeRefusesLocalChangesWithoutForce(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "dirty")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "dirty"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(t.Context(), r.repo, target, false); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("err = %v, want ErrWorktreeDirty", err)
	}
	if err := RemoveWorktree(t.Context(), r.repo, target, true); err != nil {
		t.Fatalf("force removal returned error %v", err)
	}
}

func TestRemoveWorktreeRefusesTheMainOne(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	if err := RemoveWorktree(t.Context(), r.repo, r.dir, true); !errors.Is(err, ErrWorktreeIsMain) {
		t.Fatalf("err = %v, want ErrWorktreeIsMain", err)
	}
}

func TestRemoveWorktreeReportsAPathThatIsNotAWorktree(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	err := RemoveWorktree(t.Context(), r.repo, worktreePath(t, "nowhere"), false)

	if !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("err = %v, want ErrWorktreeNotFound", err)
	}
}

func TestLockedWorktreeIsKeptUntilItIsUnlocked(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "locked")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "locked"}); err != nil {
		t.Fatal(err)
	}

	if err := LockWorktree(r.repo, target, "busy with a build"); err != nil {
		t.Fatalf("LockWorktree returned error %v", err)
	}
	list, err := ListWorktrees(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !list[1].Locked || list[1].LockReason != "busy with a build" {
		t.Fatalf("worktree = %+v, want it locked with the reason", list[1])
	}
	if err := RemoveWorktree(t.Context(), r.repo, target, false); !errors.Is(err, ErrWorktreeLocked) {
		t.Fatalf("err = %v, want ErrWorktreeLocked", err)
	}

	if err := UnlockWorktree(r.repo, target); err != nil {
		t.Fatalf("UnlockWorktree returned error %v", err)
	}
	if err := UnlockWorktree(r.repo, target); err != nil {
		t.Fatalf("unlocking twice returned error %v", err)
	}
	if err := RemoveWorktree(t.Context(), r.repo, target, false); err != nil {
		t.Fatalf("RemoveWorktree after unlocking returned error %v", err)
	}
}

func TestLockingTheMainWorktreeIsRefused(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	if err := LockWorktree(r.repo, r.dir, "no"); !errors.Is(err, ErrWorktreeIsMain) {
		t.Fatalf("err = %v, want ErrWorktreeIsMain", err)
	}
}

func TestPruneWorktreesDropsTheAdministrativeCopyOfADeletedDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "vanished")
	wt, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "vanished"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}

	dry, err := PruneWorktrees(r.repo, PruneWorktreesOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run returned error %v", err)
	}
	if len(dry) != 1 || dry[0] != wt.ID {
		t.Fatalf("dry run = %v, want the vanished worktree", dry)
	}
	if _, err := os.Stat(filepath.Join(r.repo.CommonDir(), worktreesDir, wt.ID)); err != nil {
		t.Fatal("a dry run must not delete anything")
	}

	pruned, err := PruneWorktrees(r.repo, PruneWorktreesOptions{})
	if err != nil {
		t.Fatalf("PruneWorktrees returned error %v", err)
	}
	if len(pruned) != 1 {
		t.Fatalf("pruned = %v, want one", pruned)
	}
	if _, err := os.Stat(filepath.Join(r.repo.CommonDir(), worktreesDir, wt.ID)); !os.IsNotExist(err) {
		t.Fatalf("administrative directory survived pruning: %v", err)
	}
}

func TestPruneWorktreesKeepsALockedOne(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "kept")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "kept"}); err != nil {
		t.Fatal(err)
	}
	if err := LockWorktree(r.repo, target, "on a removable disk"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneWorktrees(r.repo, PruneWorktreesOptions{})

	if err != nil {
		t.Fatalf("PruneWorktrees returned error %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want a locked worktree left alone", pruned)
	}
}

func TestMoveWorktreeCarriesTheLinkAlong(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	from := worktreePath(t, "before")
	wt, err := AddWorktree(t.Context(), r.repo, from, AddWorktreeOptions{Branch: "moving"})
	if err != nil {
		t.Fatal(err)
	}
	to := worktreePath(t, "after")

	if err := MoveWorktree(r.repo, from, to); err != nil {
		t.Fatalf("MoveWorktree returned error %v", err)
	}

	admin := filepath.Join(r.repo.CommonDir(), worktreesDir, wt.ID)
	if got := readTrimmedFile(t, filepath.Join(admin, worktreeGitDir)); got != filepath.ToSlash(filepath.Join(to, dotGitName)) {
		t.Fatalf("gitdir = %q, want the new location", got)
	}
	if got := readTrimmedFile(t, filepath.Join(to, dotGitName)); got != gitFileMarker+filepath.ToSlash(admin) {
		t.Fatalf(".git file = %q, want it to still point at the admin directory", got)
	}
	list, err := ListWorktrees(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if list[1].Path != to || list[1].Prunable {
		t.Fatalf("worktree after the move = %+v", list[1])
	}
}

func TestMovingTheMainWorktreeOrALockedOneIsRefused(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	from := worktreePath(t, "stay")
	if _, err := AddWorktree(t.Context(), r.repo, from, AddWorktreeOptions{Branch: "stay"}); err != nil {
		t.Fatal(err)
	}

	if err := MoveWorktree(r.repo, r.dir, worktreePath(t, "x")); !errors.Is(err, ErrWorktreeIsMain) {
		t.Fatalf("err = %v, want ErrWorktreeIsMain", err)
	}
	if err := LockWorktree(r.repo, from, "pinned"); err != nil {
		t.Fatal(err)
	}
	if err := MoveWorktree(r.repo, from, worktreePath(t, "y")); !errors.Is(err, ErrWorktreeLocked) {
		t.Fatalf("err = %v, want ErrWorktreeLocked", err)
	}
}

func TestWorktreeIDIsBuiltFromTheDirectoryName(t *testing.T) {
	cases := map[string]string{
		"feature":      "feature",
		"my feature":   "my-feature",
		"../escape":    "escape",
		"":             "worktree",
		"...":          "worktree",
		"UPPER_case-1": "UPPER_case-1",
	}
	for in, want := range cases {
		if got := sanitiseWorktreeID(in); got != want {
			t.Fatalf("sanitiseWorktreeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func readTrimmedFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
}

func TestAFreshWorktreeHasNoLocalChanges(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "clean")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "clean"}); err != nil {
		t.Fatal(err)
	}

	dirty, err := worktreeIsDirty(t.Context(), target)

	if err != nil {
		t.Fatalf("worktreeIsDirty returned error %v", err)
	}
	if dirty {
		t.Fatal("a worktree that was just checked out must be clean, its .git link included")
	}
}
