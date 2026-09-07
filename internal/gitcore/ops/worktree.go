package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

var (
	ErrWorktreeExists     = errors.New("ops: worktree already exists")
	ErrWorktreeNotFound   = errors.New("ops: worktree not found")
	ErrWorktreeLocked     = errors.New("ops: worktree is locked")
	ErrWorktreeIsMain     = errors.New("ops: the main worktree cannot be removed")
	ErrWorktreeTargetBusy = errors.New("ops: target directory exists and is not empty")
	ErrWorktreeDirty      = errors.New("ops: worktree has local changes")
)

const (
	worktreesDir      = "worktrees"
	worktreeHeadFile  = "HEAD"
	worktreeGitDir    = "gitdir"
	worktreeCommonDir = "commondir"
	worktreeLockFile  = "locked"
	dotGitName        = ".git"
	gitFileMarker     = "gitdir: "
	commonDirTarget   = "../.."
	worktreeFileMode  = 0o666
)

var (
	worktreeMkdirAll  = os.MkdirAll
	worktreeWriteFile = os.WriteFile
	worktreeReadFile  = os.ReadFile
	worktreeReadDir   = os.ReadDir
	worktreeStat      = os.Stat
	worktreeRemoveAll = os.RemoveAll
	worktreeRemove    = os.Remove
	worktreeRename    = os.Rename
	worktreeRepoOpen  = repo.Open
	worktreeRefsOpen  = refs.Open
	worktreeOpen      = worktree.Open
)

type Worktree struct {
	ID          string
	Path        string
	Head        hash.ObjectID
	Branch      refs.Name
	Main        bool
	Bare        bool
	Detached    bool
	Locked      bool
	LockReason  string
	Prunable    bool
	PruneReason string
}

func ListWorktrees(r *repo.Repository) ([]Worktree, error) {
	main, err := mainWorktree(r)
	if err != nil {
		return nil, err
	}
	linked, err := linkedWorktrees(r)
	if err != nil {
		return nil, err
	}
	return append([]Worktree{main}, linked...), nil
}

func mainWorktree(r *repo.Repository) (Worktree, error) {
	wt := Worktree{Path: r.WorkTree(), Main: true, Bare: r.IsBare()}
	if wt.Bare {
		wt.Path = r.CommonDir()
	}
	head, branch, err := headOfGitDir(r.CommonDir(), r.CommonDir())
	if err != nil {
		return Worktree{}, err
	}
	wt.Head, wt.Branch = head, branch
	wt.Detached = branch == ""
	return wt, nil
}

func linkedWorktrees(r *repo.Repository) ([]Worktree, error) {
	dir := filepath.Join(r.CommonDir(), worktreesDir)
	entries, err := worktreeReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]Worktree, 0, len(names))
	for _, name := range names {
		wt, err := readLinkedWorktree(dir, r.CommonDir(), name)
		if err != nil {
			return nil, err
		}
		out = append(out, wt)
	}
	return out, nil
}

func readLinkedWorktree(worktreesRoot, commonDir, id string) (Worktree, error) {
	admin := filepath.Join(worktreesRoot, id)
	wt := Worktree{ID: id}
	gitFile, err := readTrimmed(filepath.Join(admin, worktreeGitDir))
	if err != nil {
		return Worktree{}, err
	}
	wt.Path = filepath.Dir(filepath.FromSlash(gitFile))
	head, branch, err := headOfGitDir(admin, commonDir)
	if err != nil {
		return Worktree{}, err
	}
	wt.Head, wt.Branch, wt.Detached = head, branch, branch == ""
	if reason, locked, err := worktreeLock(admin); err != nil {
		return Worktree{}, err
	} else if locked {
		wt.Locked, wt.LockReason = true, reason
	}
	if _, err := worktreeStat(filepath.FromSlash(gitFile)); err != nil {
		wt.Prunable = true
		wt.PruneReason = fmt.Sprintf("gitdir file points to a missing location: %s", gitFile)
	}
	return wt, nil
}

func worktreeLock(admin string) (string, bool, error) {
	data, err := worktreeReadFile(filepath.Join(admin, worktreeLockFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

func headOfGitDir(gitDir, commonDir string) (hash.ObjectID, refs.Name, error) {
	store, err := worktreeRefsOpen(refs.Options{GitDir: gitDir, CommonDir: commonDir})
	if err != nil {
		return hash.Zero, "", err
	}
	defer func() { _ = store.Close() }()
	branch, commit, err := currentHeadState(store)
	if err != nil {
		return hash.Zero, "", err
	}
	return commit, branch, nil
}

func readTrimmed(path string) (string, error) {
	data, err := worktreeReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func findWorktree(r *repo.Repository, path string) (Worktree, []Worktree, error) {
	list, err := ListWorktrees(r)
	if err != nil {
		return Worktree{}, nil, err
	}
	wanted := cleanWorktreePath(path)
	for _, wt := range list {
		if samePath(wt.Path, wanted) || wt.ID == path {
			return wt, list, nil
		}
	}
	return Worktree{}, nil, fmt.Errorf("%w: %s", ErrWorktreeNotFound, path)
}

func cleanWorktreePath(path string) string {
	abs, _ := filepath.Abs(path)
	return filepath.Clean(abs)
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func adminDir(r *repo.Repository, id string) string {
	return filepath.Join(r.CommonDir(), worktreesDir, id)
}
