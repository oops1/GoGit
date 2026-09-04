package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	indexFileName = "index"
	indexLockName = "index.lock"
)

type indexLock struct {
	root *os.Root
	file *os.File
	idx  *index.Index
}

func lockIndex(r *repo.Repository) (*indexLock, error) {
	root := r.Root()
	file, err := fsRootOpenFile(root, indexLockName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%w: %s", ErrIndexLocked, r.IndexFile())
	}
	if err != nil {
		return nil, fmt.Errorf("ops: create %s: %w", indexLockName, err)
	}
	idx, err := readIndex(r)
	if err != nil {
		_ = file.Close()
		_ = fsRootRemove(root, indexLockName)
		return nil, err
	}
	return &indexLock{root: root, file: file, idx: idx}, nil
}

func readIndex(r *repo.Repository) (*index.Index, error) {
	idx, err := index.ReadFile(r.IndexFile())
	if errors.Is(err, fs.ErrNotExist) {
		return index.New(index.Version2), nil
	}
	if err != nil {
		return nil, err
	}
	return idx, nil
}

func (l *indexLock) abort() {
	_ = l.file.Close()
	_ = fsRootRemove(l.root, indexLockName)
}

func (l *indexLock) commit() error {
	if err := idxWrite(l.idx, l.file, 0); err != nil {
		l.abort()
		return err
	}
	if err := fsFileClose(l.file); err != nil {
		_ = fsRootRemove(l.root, indexLockName)
		return fmt.Errorf("ops: close %s: %w", indexLockName, err)
	}
	if err := fsRootRename(l.root, indexLockName, indexFileName); err != nil {
		_ = fsRootRemove(l.root, indexLockName)
		return fmt.Errorf("ops: rename %s: %w", indexLockName, err)
	}
	return nil
}
