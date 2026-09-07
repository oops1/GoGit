package ops

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

type PruneWorktreesOptions struct {
	DryRun bool
}

func RemoveWorktree(ctx context.Context, r *repo.Repository, path string, force bool) error {
	wt, _, err := findWorktree(r, path)
	if err != nil {
		return err
	}
	if wt.Main {
		return fmt.Errorf("%w: %s", ErrWorktreeIsMain, wt.Path)
	}
	if wt.Locked && !force {
		return fmt.Errorf("%w: %s: %s", ErrWorktreeLocked, wt.Path, wt.LockReason)
	}
	if !force && !wt.Prunable {
		dirty, err := worktreeIsDirty(ctx, wt.Path)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("%w: %s", ErrWorktreeDirty, wt.Path)
		}
	}
	if !wt.Prunable {
		if err := worktreeRemoveAll(wt.Path); err != nil {
			return err
		}
	}
	return worktreeRemoveAll(adminDir(r, wt.ID))
}

func worktreeIsDirty(ctx context.Context, path string) (bool, error) {
	r, err := worktreeRepoOpen(path, repo.OpenOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Close() }()
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	tree, err := worktreeOpen(r, worktree.Options{DB: db})
	if err != nil {
		return false, err
	}
	defer func() { _ = tree.Close() }()
	status, err := tree.Status(ctx)
	if err != nil {
		return false, err
	}
	for _, e := range status.Entries {
		if e.Staged != worktree.StatusUnmodified || e.Unstaged != worktree.StatusUnmodified {
			if e.Unstaged == worktree.StatusIgnored {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func PruneWorktrees(r *repo.Repository, opts PruneWorktreesOptions) ([]string, error) {
	list, err := linkedWorktrees(r)
	if err != nil {
		return nil, err
	}
	var pruned []string
	for _, wt := range list {
		if !wt.Prunable || wt.Locked {
			continue
		}
		pruned = append(pruned, wt.ID)
		if opts.DryRun {
			continue
		}
		if err := worktreeRemoveAll(adminDir(r, wt.ID)); err != nil {
			return nil, err
		}
	}
	return pruned, nil
}

func LockWorktree(r *repo.Repository, path, reason string) error {
	wt, _, err := findWorktree(r, path)
	if err != nil {
		return err
	}
	if wt.Main {
		return fmt.Errorf("%w: %s", ErrWorktreeIsMain, wt.Path)
	}
	return worktreeWriteFile(filepath.Join(adminDir(r, wt.ID), worktreeLockFile), []byte(reason), worktreeFileMode)
}

func UnlockWorktree(r *repo.Repository, path string) error {
	wt, _, err := findWorktree(r, path)
	if err != nil {
		return err
	}
	if !wt.Locked {
		return nil
	}
	return worktreeRemove(filepath.Join(adminDir(r, wt.ID), worktreeLockFile))
}

func MoveWorktree(r *repo.Repository, from, to string) error {
	wt, _, err := findWorktree(r, from)
	if err != nil {
		return err
	}
	if wt.Main {
		return fmt.Errorf("%w: %s", ErrWorktreeIsMain, wt.Path)
	}
	if wt.Locked {
		return fmt.Errorf("%w: %s: %s", ErrWorktreeLocked, wt.Path, wt.LockReason)
	}
	target := cleanWorktreePath(to)
	if err := checkWorktreeTarget(r, target); err != nil {
		return err
	}
	if err := worktreeRename(wt.Path, target); err != nil {
		return err
	}
	admin := adminDir(r, wt.ID)
	gitFile := filepath.Join(target, dotGitName)
	if err := worktreeWriteFile(filepath.Join(admin, worktreeGitDir), []byte(filepath.ToSlash(gitFile)+"\n"), worktreeFileMode); err != nil {
		return err
	}
	return worktreeWriteFile(gitFile, []byte(gitFileMarker+filepath.ToSlash(admin)+"\n"), worktreeFileMode)
}
