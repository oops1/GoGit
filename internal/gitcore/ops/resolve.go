package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type ConflictSide int

const (
	TakeOurs ConflictSide = iota
	TakeTheirs
)

var ErrNotConflicted = errors.New("ops: path has no conflict")

func (s ConflictSide) stage() index.Stage {
	if s == TakeTheirs {
		return index.StageTheirs
	}
	return index.StageOurs
}

func ResolveConflicts(ctx context.Context, r *repo.Repository, paths []string, side ConflictSide) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	lock, err := lockIndex(r)
	if err != nil {
		return err
	}
	sw := &switcher{ctx: ctx, wt: wt, db: db, format: db.Format()}
	for _, p := range paths {
		if err := resolveConflict(sw, lock.idx, p, side); err != nil {
			lock.abort()
			return err
		}
	}
	return lock.commit()
}

func resolveConflict(sw *switcher, idx *index.Index, raw string, side ConflictSide) error {
	if err := sw.ctx.Err(); err != nil {
		return err
	}
	rel, err := cleanRepoPath(raw)
	if err != nil {
		return err
	}
	stages := idx.Conflicts(rel)
	if len(stages) == 0 {
		return fmt.Errorf("%w: %s", ErrNotConflicted, rel)
	}
	idx.Remove(rel)
	for _, entry := range stages {
		if entry.Stage != side.stage() {
			continue
		}
		if err := sw.checkout(rel, treeEntry{mode: entry.Mode, id: entry.ID}); err != nil {
			return err
		}
		idx.Add(index.Entry{Path: rel, Mode: entry.Mode, ID: entry.ID, Stage: index.StageMerged})
		return nil
	}
	if err := fsRootRemove(sw.wt.root, filepath.FromSlash(rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
