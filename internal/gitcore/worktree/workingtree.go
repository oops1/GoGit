package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var (
	fsLstatFile    = (*os.Root).Lstat
	fsReadlinkFile = (*os.Root).Readlink
	fsReadFileFile = (*os.Root).ReadFile
)

func (w *Worktree) unstagedStatuses(ctx context.Context, entries []*index.Entry) (map[string]StatusCode, error) {
	results := make(map[string]StatusCode, len(entries))
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
			code, err := w.compareToWorktree(entry)
			if err != nil {
				fail(err)
				return
			}
			if code == StatusUnmodified {
				return
			}
			mu.Lock()
			results[entry.Path] = code
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
	name := filepath.FromSlash(entry.Path)
	fi, err := fsLstatFile(w.root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return StatusDeleted, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
	}
	wantKind, actualKind := kindOfMode(entry.Mode), kindOfInfo(fi)
	if wantKind != actualKind {
		return StatusTypeChanged, nil
	}
	if w.index.MatchesFile(entry, fi) {
		if w.modeChanged(entry, fi) {
			return StatusModified, nil
		}
		return StatusUnmodified, nil
	}
	switch wantKind {
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
	policy := w.textPolicy(path)
	if policy.Convert.OnCheckin != attributes.ConvertLF {
		return data
	}
	if policy.Convert.Detect && attributes.IsBinaryContent(data) {
		return data
	}
	if !bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}
