package ops

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var ErrPartialConflict = errors.New("ops: a conflicted path cannot be changed in parts")

func PatchIndex(ctx context.Context, r *repo.Repository, path string, hunks []diff.Hunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rel, err := cleanRepoPath(path)
	if err != nil {
		return err
	}
	if len(hunks) == 0 {
		return nil
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
	if err := patchIndexEntry(wt, db, lock.idx, rel, hunks); err != nil {
		lock.abort()
		return err
	}
	return lock.commit()
}

func patchIndexEntry(wt *workingTree, db *odb.DB, idx *index.Index, rel string, hunks []diff.Hunk) error {
	if len(idx.Conflicts(rel)) > 0 {
		return fmt.Errorf("%w: %s", ErrPartialConflict, rel)
	}
	mode, base, err := indexedContent(wt, db, idx, rel)
	if err != nil {
		return err
	}
	data, err := diff.Apply(base, hunks)
	if err != nil {
		return fmt.Errorf("ops: %s: %w", rel, err)
	}
	id, err := dbPut(db, object.TypeBlob, data)
	if err != nil {
		return err
	}
	idx.Add(index.Entry{
		Path:  rel,
		Mode:  mode,
		ID:    id,
		Stage: index.StageMerged,
		Stat:  index.Stat{Size: uint32(len(data))},
	})
	return nil
}

func indexedContent(wt *workingTree, db *odb.DB, idx *index.Index, rel string) (object.Mode, []byte, error) {
	if entry, found := idx.Get(rel, index.StageMerged); found {
		_, data, err := dbGet(db, entry.ID)
		return entry.Mode, data, err
	}
	info, err := fsRootLstat(wt.root, filepath.FromSlash(rel))
	if err != nil {
		return 0, nil, err
	}
	mode, _, err := (&stager{wt: wt}).readWorktreeObject(rel, info)
	return mode, nil, err
}

func PatchWorkingTree(ctx context.Context, r *repo.Repository, path string, hunks []diff.Hunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rel, err := cleanRepoPath(path)
	if err != nil {
		return err
	}
	if len(hunks) == 0 {
		return nil
	}
	base, err := workingContent(r, rel)
	if err != nil {
		return err
	}
	data, err := diff.Apply(base, hunks)
	if err != nil {
		return fmt.Errorf("ops: %s: %w", rel, err)
	}
	return writeWorkingFile(r, rel, data)
}

func workingContent(r *repo.Repository, rel string) ([]byte, error) {
	wt, err := openWorkingTree(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = wt.close() }()
	data, err := fsRootReadFile(wt.root, filepath.FromSlash(rel))
	if missingPath(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return wt.checkinConvert(rel, data), nil
}
