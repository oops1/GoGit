package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type stager struct {
	ctx  context.Context
	wt   *workingTree
	db   *odb.DB
	idx  *index.Index
	opts StageOptions
}

func Stage(ctx context.Context, r *repo.Repository, paths []string, opts StageOptions) error {
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

	s := &stager{ctx: ctx, wt: wt, db: db, idx: lock.idx, opts: opts}
	for _, p := range paths {
		clean, err := cleanRepoPath(p)
		if err != nil {
			lock.abort()
			return err
		}
		if err := s.stage(clean); err != nil {
			lock.abort()
			return err
		}
	}
	if !lock.idx.HasConflicts() {
		if _, err := lock.idx.WriteTree(db); err != nil {
			lock.abort()
			return err
		}
	}
	return lock.commit()
}

func (s *stager) stage(rel string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	info, err := fsRootLstat(s.wt.root, filepath.FromSlash(rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s.stageMissing(rel)
	case err != nil:
		return err
	case info.IsDir():
		return s.stageDir(rel)
	default:
		if !s.opts.Force && s.wt.isIgnored(rel, false) {
			return nil
		}
		return s.stageEntry(rel, info)
	}
}

func (s *stager) stageMissing(rel string) error {
	s.idx.Remove(rel)
	prefix := rel + "/"
	for _, p := range slices.Collect(s.idx.Paths(prefix)) {
		s.idx.Remove(p)
	}
	return nil
}

func (s *stager) stageDir(rel string) error {
	if !s.opts.Force && s.wt.isIgnored(rel, true) {
		return nil
	}
	entries, err := readDirRoot(s.wt.root, rel)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, entry := range entries {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		child := joinRel(rel, entry.Name())
		if entry.IsDir() {
			if err := s.stageDir(child); err != nil {
				return err
			}
			continue
		}
		if !s.opts.Force && s.wt.isIgnored(child, false) {
			continue
		}
		info, err := direntInfo(entry)
		if err != nil {
			return err
		}
		if err := s.stageEntry(child, info); err != nil {
			return err
		}
		present[child] = true
	}
	prefix := rel + "/"
	for _, tracked := range slices.Collect(s.idx.Paths(prefix)) {
		if !present[tracked] {
			if _, err := fsRootLstat(s.wt.root, filepath.FromSlash(tracked)); errors.Is(err, fs.ErrNotExist) {
				s.idx.Remove(tracked)
			}
		}
	}
	return nil
}

func (s *stager) stageEntry(rel string, info fs.FileInfo) error {
	mode, data, err := s.readWorktreeObject(rel, info)
	if err != nil {
		return err
	}
	id, err := dbPut(s.db, object.TypeBlob, data)
	if err != nil {
		return err
	}
	entry := index.Entry{
		Path:  rel,
		Mode:  mode,
		ID:    id,
		Stage: index.StageMerged,
		Stat:  statOf(info, len(data)),
	}
	s.idx.Add(entry)
	return nil
}

func (s *stager) readWorktreeObject(rel string, info fs.FileInfo) (object.Mode, []byte, error) {
	name := filepath.FromSlash(rel)
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := fsRootReadlink(s.wt.root, name)
		if err != nil {
			return 0, nil, err
		}
		return object.ModeSymlink, []byte(target), nil
	}
	data, err := fsRootReadFile(s.wt.root, name)
	if err != nil {
		return 0, nil, err
	}
	data = s.wt.checkinConvert(rel, data)
	if s.wt.fileMode && info.Mode().Perm()&0o111 != 0 {
		return object.ModeExecutable, data, nil
	}
	return object.ModeBlob, data, nil
}

func statOf(info fs.FileInfo, size int) index.Stat {
	return index.Stat{
		CTime: info.ModTime(),
		MTime: info.ModTime(),
		Size:  uint32(size),
	}
}
