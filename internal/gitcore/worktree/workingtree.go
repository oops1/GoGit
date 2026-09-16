package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var (
	fsLstatFile    = (*os.Root).Lstat
	fsReadlinkFile = (*os.Root).Readlink
	fsReadFileFile = (*os.Root).ReadFile
)

func (w *Worktree) unstagedStatuses(ctx context.Context, entries []*index.Entry) (map[string]unstagedChange, error) {
	results := make(map[string]unstagedChange, len(entries))
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		once    sync.Once
		failure error
	)
	fail := func(err error) { once.Do(func() { failure = err }) }
	sem := make(chan struct{}, w.workers)
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			change, err := w.unstagedChangeOf(ctx, entry)
			if err != nil {
				fail(err)
				return
			}
			if change.code == StatusUnmodified {
				return
			}
			mu.Lock()
			results[entry.Path] = change
			mu.Unlock()
		})
	}
	wg.Wait()
	if failure != nil {
		return nil, failure
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (w *Worktree) compareToWorktree(entry *index.Entry) (StatusCode, error) {
	if entry.SkipWorktree {
		return StatusUnmodified, nil
	}
	name := filepath.FromSlash(entry.Path)
	fi, err := fsLstatFile(w.root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return StatusDeleted, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
	}
	if fi.IsDir() && !entry.Mode.IsSubmodule() {
		return StatusDeleted, nil
	}
	wantKind, actualKind := kindOfMode(entry.Mode), kindOfInfo(fi)
	if wantKind != actualKind && !w.holdsPlainSymlink(wantKind, fi) {
		return StatusTypeChanged, nil
	}
	if w.currentIndex().MatchesFile(entry, fi, w.symlinks) {
		if w.modeChanged(entry, fi) {
			return StatusModified, nil
		}
		return StatusUnmodified, nil
	}
	if sizeChangedWithoutReading(entry, fi) {
		return StatusModified, nil
	}
	switch actualKind {
	case kindSymlink:
		target, err := fsReadlinkFile(w.root, name)
		if err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
		}
		id, err := hash.Sum(w.format, "blob", []byte(target))
		if err != nil {
			return 0, err
		}
		if id == entry.ID {
			return StatusUnmodified, nil
		}
		return StatusModified, nil
	default:
		data, err := fsReadFileFile(w.root, name)
		if err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
		}
		id, err := hash.Sum(w.format, "blob", w.convertForCheckin(entry.Path, data))
		if err != nil {
			return 0, err
		}
		if id != entry.ID {
			return StatusModified, nil
		}
		if w.modeChanged(entry, fi) {
			return StatusModified, nil
		}
		return StatusUnmodified, nil
	}
}

func sizeChangedWithoutReading(entry *index.Entry, fi os.FileInfo) bool {
	if entry.Stat.Size == 0 || uint32(fi.Size()) == entry.Stat.Size {
		return false
	}
	return fi.Mode()&os.ModeSymlink == 0 || fi.Size() != 0
}

func (w *Worktree) holdsPlainSymlink(wantKind entryKind, fi os.FileInfo) bool {
	return !w.symlinks && wantKind == kindSymlink && fi.Mode().IsRegular()
}

func (w *Worktree) fillWorkingInfo(e *Entry) {
	if e.IsDir {
		return
	}
	fi, err := fsLstatFile(w.root, filepath.FromSlash(e.Path))
	if err != nil {
		return
	}
	e.Size = fi.Size()
	e.ModTime = fi.ModTime()
}

func (w *Worktree) modeChanged(entry *index.Entry, fi os.FileInfo) bool {
	if !w.fileMode || entry.Mode.IsSymlink() || entry.Mode.IsSubmodule() {
		return false
	}
	wantExec := entry.Mode == object.ModeExecutable
	haveExec := fi.Mode().Perm()&0o111 != 0
	return wantExec != haveExec
}

func (w *Worktree) convertForCheckin(path string, data []byte) []byte {
	return w.policy(path).CompareToGit(data, func() ([]byte, bool) {
		return w.currentIndex().ContentBlob(w.db, path)
	})
}
