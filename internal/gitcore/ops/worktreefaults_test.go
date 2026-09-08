package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

func swapWorktreeReadFile(t testing.TB, replacement func(string) ([]byte, error)) {
	t.Helper()
	original := worktreeReadFile
	worktreeReadFile = replacement
	t.Cleanup(func() { worktreeReadFile = original })
}

func swapWorktreeStat(t testing.TB, replacement func(string) (fs.FileInfo, error)) {
	t.Helper()
	original := worktreeStat
	worktreeStat = replacement
	t.Cleanup(func() { worktreeStat = original })
}

func swapWorktreeReadDir(t testing.TB, replacement func(string) ([]os.DirEntry, error)) {
	t.Helper()
	original := worktreeReadDir
	worktreeReadDir = replacement
	t.Cleanup(func() { worktreeReadDir = original })
}

func swapWorktreeWriteFile(t testing.TB, replacement func(string, []byte, fs.FileMode) error) {
	t.Helper()
	original := worktreeWriteFile
	worktreeWriteFile = replacement
	t.Cleanup(func() { worktreeWriteFile = original })
}

func swapWorktreeMkdirAll(t testing.TB, replacement func(string, fs.FileMode) error) {
	t.Helper()
	original := worktreeMkdirAll
	worktreeMkdirAll = replacement
	t.Cleanup(func() { worktreeMkdirAll = original })
}

func swapWorktreeRemoveAll(t testing.TB, replacement func(string) error) {
	t.Helper()
	original := worktreeRemoveAll
	worktreeRemoveAll = replacement
	t.Cleanup(func() { worktreeRemoveAll = original })
}

func swapWorktreeRename(t testing.TB, replacement func(string, string) error) {
	t.Helper()
	original := worktreeRename
	worktreeRename = replacement
	t.Cleanup(func() { worktreeRename = original })
}

func swapWorktreeRepoOpen(t testing.TB, replacement func(string, repo.OpenOptions) (*repo.Repository, error)) {
	t.Helper()
	original := worktreeRepoOpen
	worktreeRepoOpen = replacement
	t.Cleanup(func() { worktreeRepoOpen = original })
}

func swapWorktreeRefsOpen(t testing.TB, replacement func(refs.Options) (*refs.Store, error)) {
	t.Helper()
	original := worktreeRefsOpen
	worktreeRefsOpen = replacement
	t.Cleanup(func() { worktreeRefsOpen = original })
}

func swapWorktreeOpen(t testing.TB, replacement func(*repo.Repository, worktree.Options) (*worktree.Worktree, error)) {
	t.Helper()
	original := worktreeOpen
	worktreeOpen = replacement
	t.Cleanup(func() { worktreeOpen = original })
}

func swapWorktreeRemove(t testing.TB, replacement func(string) error) {
	t.Helper()
	original := worktreeRemove
	worktreeRemove = replacement
	t.Cleanup(func() { worktreeRemove = original })
}

func worktreeRepoWithOne(t *testing.T) (*testRepo, string) {
	t.Helper()
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "linked")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "linked"}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}
	return r, target
}

func TestListWorktreesReportsAHeadItCannotRead(t *testing.T) {
	r := newTestRepo(t)
	swapWorktreeRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })

	if _, err := ListWorktrees(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestListWorktreesReportsADirectoryItCannotRead(t *testing.T) {
	r := newTestRepo(t)
	swapWorktreeReadDir(t, func(string) ([]os.DirEntry, error) { return nil, errInjected })

	if _, err := ListWorktrees(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestListWorktreesReportsAnAdministrativeFileItCannotRead(t *testing.T) {
	r, _ := worktreeRepoWithOne(t)
	swapWorktreeReadFile(t, func(string) ([]byte, error) { return nil, errInjected })

	if _, err := ListWorktrees(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestListWorktreesReportsALockItCannotRead(t *testing.T) {
	r, _ := worktreeRepoWithOne(t)
	original := worktreeReadFile
	swapWorktreeReadFile(t, func(path string) ([]byte, error) {
		if filepath.Base(path) == worktreeLockFile {
			return nil, errInjected
		}
		return original(path)
	})

	if _, err := ListWorktrees(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestListWorktreesReportsAHeadOfALinkedWorktreeItCannotRead(t *testing.T) {
	r, _ := worktreeRepoWithOne(t)
	original := worktreeRefsOpen
	calls := 0
	swapWorktreeRefsOpen(t, func(opts refs.Options) (*refs.Store, error) {
		calls++
		if calls > 1 {
			return nil, errInjected
		}
		return original(opts)
	})

	if _, err := ListWorktrees(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeStopsOnACancelledContext(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := AddWorktree(ctx, r.repo, worktreePath(t, "never"), AddWorktreeOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
}

func TestAddWorktreeReportsATargetItCannotInspect(t *testing.T) {
	r := newTestRepo(t)
	swapWorktreeReadDir(t, func(path string) ([]os.DirEntry, error) {
		if filepath.Base(path) == worktreesDir {
			return nil, os.ErrNotExist
		}
		return nil, errInjected
	})

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "unreadable"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsAStartPointItCannotResolve(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "bad"), AddWorktreeOptions{StartPoint: "no-such-thing"})

	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v, want ErrTargetNotFound", err)
	}
}

func TestAddWorktreeReportsABranchItCannotCreate(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "taken"), AddWorktreeOptions{Branch: "main"})

	if !errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v, want ErrBranchExists", err)
	}
}

func TestAddWorktreeReportsAnObjectDatabaseItCannotOpen(t *testing.T) {
	r := newTestRepo(t)
	original := odbOpen
	odbOpen = func(string, odb.Options) (*odb.DB, error) { return nil, errInjected }
	t.Cleanup(func() { odbOpen = original })

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "nodb"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsARefStoreItCannotOpen(t *testing.T) {
	r := newTestRepo(t)
	original := refsOpen
	refsOpen = func(refs.Options) (*refs.Store, error) { return nil, errInjected }
	t.Cleanup(func() { refsOpen = original })

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "norefs"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsAnAdministrativeDirectoryItCannotProbe(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	swapWorktreeStat(t, func(string) (fs.FileInfo, error) { return nil, errInjected })

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "noprobe"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsADirectoryItCannotCreate(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	swapWorktreeMkdirAll(t, func(string, fs.FileMode) error { return errInjected })

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "nodir"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsAFileItCannotWrite(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	swapWorktreeWriteFile(t, func(string, []byte, fs.FileMode) error { return errInjected })

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "nowrite"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsAWorktreeItCannotOpenForCheckout(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	swapWorktreeRepoOpen(t, func(string, repo.OpenOptions) (*repo.Repository, error) { return nil, errInjected })

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "noopen"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeLeavesTheFilesAloneWhenAskedTo(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "empty")

	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Detach: true, NoCheckout: true}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt was checked out anyway: %v", err)
	}
}

func TestRemoveWorktreeReportsTheDirectoryItCannotDelete(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeRemoveAll(t, func(string) error { return errInjected })

	if err := RemoveWorktree(t.Context(), r.repo, target, true); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestRemoveWorktreeReportsAStatusItCannotRead(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeOpen(t, func(*repo.Repository, worktree.Options) (*worktree.Worktree, error) { return nil, errInjected })

	if err := RemoveWorktree(t.Context(), r.repo, target, false); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestRemoveWorktreeReportsARepositoryItCannotOpen(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeRepoOpen(t, func(string, repo.OpenOptions) (*repo.Repository, error) { return nil, errInjected })

	if err := RemoveWorktree(t.Context(), r.repo, target, false); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestRemoveWorktreeReportsAnObjectDatabaseItCannotOpen(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	original := odbOpen
	odbOpen = func(dir string, opts odb.Options) (*odb.DB, error) { return nil, errInjected }
	t.Cleanup(func() { odbOpen = original })

	if err := RemoveWorktree(t.Context(), r.repo, target, false); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestRemoveWorktreeReportsAListItCannotRead(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })

	if err := RemoveWorktree(t.Context(), r.repo, target, false); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestPruneWorktreesReportsAListItCannotRead(t *testing.T) {
	r, _ := worktreeRepoWithOne(t)
	swapWorktreeReadDir(t, func(string) ([]os.DirEntry, error) { return nil, errInjected })

	if _, err := PruneWorktrees(r.repo, PruneWorktreesOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestPruneWorktreesReportsAnAdministrativeCopyItCannotDelete(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	swapWorktreeRemoveAll(t, func(string) error { return errInjected })

	if _, err := PruneWorktrees(r.repo, PruneWorktreesOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestLockAndUnlockReportAWorktreeTheyCannotFind(t *testing.T) {
	r := newTestRepo(t)
	missing := worktreePath(t, "missing")

	if err := LockWorktree(r.repo, missing, "why"); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("lock err = %v, want ErrWorktreeNotFound", err)
	}
	if err := UnlockWorktree(r.repo, missing); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("unlock err = %v, want ErrWorktreeNotFound", err)
	}
	if err := MoveWorktree(r.repo, missing, worktreePath(t, "elsewhere")); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("move err = %v, want ErrWorktreeNotFound", err)
	}
}

func TestUnlockReportsALockFileItCannotDelete(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	if err := LockWorktree(r.repo, target, "held"); err != nil {
		t.Fatal(err)
	}
	swapWorktreeRemove(t, func(string) error { return errInjected })

	if err := UnlockWorktree(r.repo, target); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestMoveWorktreeReportsADirectoryItCannotRename(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeRename(t, func(string, string) error { return errInjected })

	if err := MoveWorktree(r.repo, target, worktreePath(t, "elsewhere")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestMoveWorktreeReportsALinkItCannotWrite(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeWriteFile(t, func(string, []byte, fs.FileMode) error { return errInjected })

	if err := MoveWorktree(r.repo, target, worktreePath(t, "elsewhere")); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestMoveWorktreeRefusesATargetThatIsTaken(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	busy := worktreePath(t, "busy")
	if err := os.MkdirAll(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := MoveWorktree(r.repo, target, busy); !errors.Is(err, ErrWorktreeTargetBusy) {
		t.Fatalf("err = %v, want ErrWorktreeTargetBusy", err)
	}
}

func TestLockWorktreeReportsALockFileItCannotWrite(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	swapWorktreeWriteFile(t, func(string, []byte, fs.FileMode) error { return errInjected })

	if err := LockWorktree(r.repo, target, "why"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestListWorktreesReportsTheDirectoryOfABareRepository(t *testing.T) {
	r := newBareTestRepo(t)

	list, err := ListWorktrees(r.repo)

	if err != nil {
		t.Fatalf("ListWorktrees returned error %v", err)
	}
	if !list[0].Bare || list[0].Path != r.repo.CommonDir() {
		t.Fatalf("bare worktree = %+v, want the git directory itself", list[0])
	}
}

func TestListWorktreesReportsAHeadItCannotResolve(t *testing.T) {
	r, _ := worktreeRepoWithOne(t)
	admin := adminDir(r.repo, "linked")
	if err := os.WriteFile(filepath.Join(admin, worktreeHeadFile), []byte("ref: refs/heads/../../escape\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ListWorktrees(r.repo); err == nil {
		t.Fatal("a HEAD that names an impossible ref must be reported")
	}
}

func TestAddWorktreeReportsAListItCannotReadWhileCheckingTheBranch(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	original := worktreeReadDir
	calls := 0
	swapWorktreeReadDir(t, func(path string) ([]os.DirEntry, error) {
		if filepath.Base(path) == worktreesDir {
			calls++
			if calls > 1 {
				return nil, errInjected
			}
		}
		return original(path)
	})

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "branchcheck"), AddWorktreeOptions{StartPoint: "main"})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestAddWorktreeReportsTheTargetDirectoryItCannotCreate(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	original := worktreeMkdirAll
	calls := 0
	swapWorktreeMkdirAll(t, func(path string, mode fs.FileMode) error {
		calls++
		if calls > 1 {
			return errInjected
		}
		return original(path, mode)
	})

	_, err := AddWorktree(t.Context(), r.repo, worktreePath(t, "notarget"), AddWorktreeOptions{Detach: true})

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestRemoveWorktreeReportsAStatusThatFails(t *testing.T) {
	r, target := worktreeRepoWithOne(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := RemoveWorktree(ctx, r.repo, target, false); err == nil {
		t.Fatal("a status that cannot run must be reported")
	}
}

func TestAddWorktreeAcceptsAnEmptyDirectoryThatAlreadyExists(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "prepared")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Detach: true}); err != nil {
		t.Fatalf("AddWorktree returned error %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "a.txt")); err != nil {
		t.Fatalf("the worktree was not filled in: %v", err)
	}
}

func TestAnIgnoredFileDoesNotMakeAWorktreeDirty(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "*.log\n")
	r.writeFile("a.txt", "one")
	mustStage(t, r, ".gitignore")
	mustStage(t, r, "a.txt")
	r.commitAll("first")
	target := worktreePath(t, "ignored")
	if _, err := AddWorktree(t.Context(), r.repo, target, AddWorktreeOptions{Branch: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "build.log"), []byte("noise"), 0o600); err != nil {
		t.Fatal(err)
	}

	dirty, err := worktreeIsDirty(t.Context(), target)

	if err != nil {
		t.Fatalf("worktreeIsDirty returned error %v", err)
	}
	if dirty {
		t.Fatal("an ignored file must not count as a local change")
	}
}

func swapWorktreeResolve(t testing.TB, replacement func(string) (string, error)) {
	t.Helper()
	original := worktreeResolve
	worktreeResolve = replacement
	t.Cleanup(func() { worktreeResolve = original })
}

func TestTwoNamesOfTheSameDirectoryAreOneWorktree(t *testing.T) {
	swapWorktreeResolve(t, func(path string) (string, error) {
		if short, ok := strings.CutPrefix(path, `C:\SHORT~1`); ok {
			return `C:\short name` + short, nil
		}
		return path, nil
	})

	if !samePath(`C:\SHORT~1\feature`, `C:\short name\feature`) {
		t.Fatal("the short and the long name of one directory must compare equal")
	}
}

func TestAPathThatCannotBeResolvedIsComparedAsItIs(t *testing.T) {
	swapWorktreeResolve(t, func(string) (string, error) { return "", errors.New("gone") })

	if samePath("a", "b") {
		t.Fatal("different paths must stay different when they cannot be resolved")
	}
	if !samePath("a", "a") {
		t.Fatal("one path must equal itself when it cannot be resolved")
	}
}
