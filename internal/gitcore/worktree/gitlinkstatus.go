package worktree

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var (
	nestedOpenRepository = repo.OpenLayout
	nestedOpenObjects    = odb.Open
)

type unstagedChange struct {
	code      StatusCode
	submodule SubmoduleChange
}

func (w *Worktree) unstagedChangeOf(ctx context.Context, entry *index.Entry) (unstagedChange, error) {
	code, err := w.compareToWorktree(entry)
	if err != nil || code != StatusUnmodified || entry.SkipWorktree || !entry.Mode.IsSubmodule() {
		return unstagedChange{code: code}, err
	}
	change, err := w.gitlinkChange(ctx, entry)
	if err != nil || change == (SubmoduleChange{}) {
		return unstagedChange{code: StatusUnmodified}, err
	}
	return unstagedChange{code: StatusModified, submodule: change}, nil
}

func (w *Worktree) gitlinkChange(ctx context.Context, entry *index.Entry) (SubmoduleChange, error) {
	layout, ok := repo.DiscoverWorkTree(filepath.Join(w.repo.WorkTree(), filepath.FromSlash(entry.Path)))
	if !ok {
		return SubmoduleChange{}, nil
	}
	head, headErr := gitlink.Head(layout)
	change := SubmoduleChange{CommitChanged: headErr == nil && head != entry.ID}
	modified, untracked, err := w.nestedChanges(ctx, layout)
	if err != nil {
		return SubmoduleChange{}, fmt.Errorf("%w: %s: %w", ErrReadSubmodule, entry.Path, err)
	}
	change.Modified, change.Untracked = modified, untracked
	return change, nil
}

func (w *Worktree) nestedChanges(ctx context.Context, layout repo.Layout) (modified, untracked bool, err error) {
	r, err := nestedOpenRepository(layout, w.repo.Options())
	if err != nil {
		return false, false, err
	}
	defer func() { _ = r.Close() }()
	db, err := nestedOpenObjects(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false, false, err
	}
	defer func() { _ = db.Close() }()
	nested, err := Open(r, Options{DB: db, Env: w.env, Workers: w.workers, MaxFiles: w.maxFiles})
	if err != nil {
		return false, false, err
	}
	defer func() { _ = nested.Close() }()
	status, err := nested.Status(ctx)
	if err != nil {
		return false, false, err
	}
	for _, e := range status.Entries {
		untracked = untracked || e.Unstaged == StatusUntracked || e.Submodule.Untracked
		modified = modified || recordsTrackedChange(e) && e.Submodule != SubmoduleChange{Untracked: true}
	}
	return modified, untracked, nil
}

func recordsTrackedChange(e Entry) bool {
	if e.Conflict != ConflictNone || e.Staged != StatusUnmodified {
		return true
	}
	return e.Unstaged != StatusUnmodified && e.Unstaged != StatusUntracked && e.Unstaged != StatusIgnored
}
