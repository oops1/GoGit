package ops

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/gitlink"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type stager struct {
	ctx   context.Context
	wt    *workingTree
	db    *odb.DB
	idx   *index.Index
	opts  StageOptions
	rules index.PathRules
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

	rules, err := pathRulesOf(r)
	if err != nil {
		return err
	}
	lock, err := lockIndex(r)
	if err != nil {
		return err
	}

	s := &stager{ctx: ctx, wt: wt, db: db, idx: lock.idx, opts: opts, rules: rules}
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
	case missingPath(err):
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
	s.removeUnlessSparse(rel)
	prefix := rel + "/"
	for _, p := range slices.Collect(s.idx.Paths(prefix)) {
		s.removeUnlessSparse(p)
	}
	return nil
}

func (s *stager) sparse(rel string) bool {
	entry, ok := s.idx.Get(rel, index.StageMerged)
	return ok && entry.SkipWorktree
}

func (s *stager) removeUnlessSparse(rel string) {
	if !s.sparse(rel) {
		s.idx.Remove(rel)
	}
}

func (s *stager) tracksGitlink(rel string) bool {
	if entry, ok := s.idx.Get(rel, index.StageMerged); ok {
		return entry.Mode.IsSubmodule()
	}
	return slices.ContainsFunc(s.idx.Conflicts(rel), func(stage index.Entry) bool { return stage.Mode.IsSubmodule() })
}

func (s *stager) tracksInside(rel string) bool {
	for range s.idx.Paths(rel + "/") {
		return true
	}
	return false
}

func (s *stager) stageGitlink(rel string, tracked bool) (bool, error) {
	head, found, err := gitlink.HeadOf(filepath.Join(s.wt.root.Name(), filepath.FromSlash(rel)))
	switch {
	case !found, !tracked && s.tracksInside(rel):
		return tracked, nil
	case tracked && (err != nil || s.sparse(rel)):
		return true, nil
	case err != nil:
		return true, fmt.Errorf("%w: %s", ErrNoCommitCheckedOut, rel)
	}
	s.idx.Remove(rel)
	return true, s.idx.AddVerified(index.Entry{Path: rel, Mode: object.ModeSubmodule, ID: head, Stage: index.StageMerged}, s.rules)
}

func (s *stager) stageDir(rel string) error {
	tracked := s.tracksGitlink(rel)
	if !tracked && !s.opts.Force && s.wt.isIgnored(rel, true) {
		return nil
	}
	if handled, err := s.stageGitlink(rel, tracked); handled {
		return err
	}
	entries, err := readDirRoot(s.wt.root, rel)
	if err != nil {
		return err
	}
	s.idx.Remove(rel)
	present := map[string]bool{}
	for _, entry := range entries {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if entry.Name() == gitDirName {
			continue
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
			if _, err := fsRootLstat(s.wt.root, filepath.FromSlash(tracked)); missingPath(err) {
				s.removeUnlessSparse(tracked)
			}
		}
	}
	return nil
}

func (s *stager) stageEntry(rel string, info fs.FileInfo) error {
	if s.sparse(rel) {
		return nil
	}
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
		Stat:  statOf(info),
	}
	if len(s.idx.Conflicts(rel)) > 0 {
		s.idx.Remove(rel)
	}
	for _, inside := range slices.Collect(s.idx.Paths(rel + "/")) {
		s.idx.Remove(inside)
	}
	if err := index.VerifyPath(entry.Path, entry.Mode, s.rules); err != nil {
		return err
	}
	s.idx.AddUpToDate(entry)
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
	data, err = s.wt.stageConvert(rel, data)
	if err != nil {
		return 0, nil, err
	}
	if s.keepsSymlinkMode(rel) {
		return object.ModeSymlink, data, nil
	}
	if s.wt.fileMode && info.Mode().Perm()&0o111 != 0 {
		return object.ModeExecutable, data, nil
	}
	return object.ModeBlob, data, nil
}

func (s *stager) keepsSymlinkMode(rel string) bool {
	if s.wt.symlinks {
		return false
	}
	entry, ok := s.idx.Get(rel, index.StageMerged)
	return ok && entry.Mode.IsSymlink()
}

func statOf(info fs.FileInfo) index.Stat {
	return index.Stat{
		CTime: info.ModTime(),
		MTime: info.ModTime(),
		Size:  uint32(info.Size()),
	}
}
