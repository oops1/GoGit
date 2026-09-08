package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type AddWorktreeOptions struct {
	Branch     string
	StartPoint string
	Detach     bool
	NoCheckout bool
	Force      bool
	Progress   progress.Func
}

func AddWorktree(ctx context.Context, r *repo.Repository, path string, opts AddWorktreeOptions) (Worktree, error) {
	if err := ctx.Err(); err != nil {
		return Worktree{}, err
	}
	target := cleanWorktreePath(path)
	if err := checkWorktreeTarget(r, target); err != nil {
		return Worktree{}, err
	}
	commit, branch, err := resolveWorktreeStart(r, opts)
	if err != nil {
		return Worktree{}, err
	}
	if branch != "" && !opts.Force {
		if err := checkBranchIsFree(r, branch); err != nil {
			return Worktree{}, err
		}
	}
	id, err := freeWorktreeID(r, filepath.Base(target))
	if err != nil {
		return Worktree{}, err
	}
	if err := writeWorktreeAdmin(r, id, target, commit, branch); err != nil {
		_ = worktreeRemoveAll(adminDir(r, id))
		return Worktree{}, err
	}
	if err := checkoutNewWorktree(ctx, target, commit, opts); err != nil {
		_ = worktreeRemoveAll(adminDir(r, id))
		return Worktree{}, err
	}
	return Worktree{
		ID:       id,
		Path:     target,
		Head:     commit,
		Branch:   branch,
		Detached: branch == "",
	}, nil
}

func checkWorktreeTarget(r *repo.Repository, target string) error {
	if _, _, err := findWorktree(r, target); err == nil {
		return fmt.Errorf("%w: %s", ErrWorktreeExists, target)
	}
	entries, err := worktreeReadDir(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s", ErrWorktreeTargetBusy, target)
	}
	return nil
}

func resolveWorktreeStart(r *repo.Repository, opts AddWorktreeOptions) (hash.ObjectID, refs.Name, error) {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return hash.Zero, "", err
	}
	defer func() { _ = db.Close() }()
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		return hash.Zero, "", err
	}
	defer func() { _ = store.Close() }()

	start := opts.StartPoint
	if start == "" {
		start = string(refs.HEAD)
	}
	commit, ref, err := resolveSwitchTarget(start, db, store)
	if err != nil {
		return hash.Zero, "", err
	}
	if opts.Branch != "" {
		branch := refs.BranchName(opts.Branch)
		if err := createWorktreeBranch(r, opts.Branch, commit); err != nil {
			return hash.Zero, "", err
		}
		return commit, branch, nil
	}
	if opts.Detach {
		return commit, "", nil
	}
	return commit, ref, nil
}

func createWorktreeBranch(r *repo.Repository, name string, start hash.ObjectID) error {
	return CreateBranch(context.Background(), r, name, start, CreateBranchOptions{})
}

func checkBranchIsFree(r *repo.Repository, branch refs.Name) error {
	list, err := ListWorktrees(r)
	if err != nil {
		return err
	}
	for _, wt := range list {
		if wt.Branch == branch {
			return fmt.Errorf("%w: %s is checked out at %s", ErrBranchCheckedOut, branch.Short(), wt.Path)
		}
	}
	return nil
}

func freeWorktreeID(r *repo.Repository, base string) (string, error) {
	base = sanitiseWorktreeID(base)
	for attempt := 0; ; attempt++ {
		id := base
		if attempt > 0 {
			id = base + strconv.Itoa(attempt)
		}
		_, err := worktreeStat(adminDir(r, id))
		if os.IsNotExist(err) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
	}
}

func sanitiseWorktreeID(base string) string {
	base = strings.TrimSpace(base)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, ".-")
	if base == "" {
		return "worktree"
	}
	return base
}

func writeWorktreeAdmin(r *repo.Repository, id, target string, commit hash.ObjectID, branch refs.Name) error {
	admin := adminDir(r, id)
	if err := worktreeMkdirAll(admin, directoryMode); err != nil {
		return err
	}
	if err := worktreeMkdirAll(target, directoryMode); err != nil {
		return err
	}
	gitFile := filepath.Join(target, dotGitName)
	files := []struct {
		path string
		data string
	}{
		{filepath.Join(admin, worktreeCommonDir), commonDirTarget + "\n"},
		{filepath.Join(admin, worktreeGitDir), filepath.ToSlash(gitFile) + "\n"},
		{filepath.Join(admin, worktreeHeadFile), worktreeHeadContent(commit, branch)},
		{gitFile, gitFileMarker + filepath.ToSlash(admin) + "\n"},
	}
	for _, f := range files {
		if err := worktreeWriteFile(f.path, []byte(f.data), worktreeFileMode); err != nil {
			return err
		}
	}
	return nil
}

func worktreeHeadContent(commit hash.ObjectID, branch refs.Name) string {
	if branch != "" {
		return "ref: " + string(branch) + "\n"
	}
	return commit.String() + "\n"
}

func checkoutNewWorktree(ctx context.Context, target string, commit hash.ObjectID, opts AddWorktreeOptions) error {
	if opts.NoCheckout {
		return nil
	}
	wt, err := worktreeRepoOpen(target, repo.OpenOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = wt.Close() }()
	return CheckoutTree(ctx, wt, commit, CheckoutOptions{Force: true, Progress: opts.Progress})
}
