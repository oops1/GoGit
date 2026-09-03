package refs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	filePerm = 0o666
	dirPerm  = 0o777
)

var (
	fsLstat    = func(root *os.Root, name string) (fs.FileInfo, error) { return root.Lstat(name) }
	fsReadFile = func(root *os.Root, name string) ([]byte, error) { return root.ReadFile(name) }
	fsRename   = func(root *os.Root, from, to string) error { return root.Rename(from, to) }
	fsRemove   = func(root *os.Root, name string) error { return root.Remove(name) }
	fsWrite    = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
)

type tree struct {
	root *os.Root
}

func openTree(dir string) (tree, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return tree{}, err
	}
	return tree{root: root}, nil
}

func (t tree) close() error { return t.root.Close() }

func (t tree) read(rel string) ([]byte, error) {
	info, err := fsLstat(t.root, filepath.FromSlash(rel))
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
	}
	data, err := fsReadFile(t.root, filepath.FromSlash(rel))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReadFailed, rel, err)
	}
	return data, nil
}

func (t tree) isFile(rel string) bool {
	info, err := t.root.Lstat(filepath.FromSlash(rel))
	return err == nil && !info.IsDir()
}

func (t tree) create(rel string, flag int) (*os.File, error) {
	name := filepath.FromSlash(rel)
	file, err := t.root.OpenFile(name, flag, filePerm)
	if !errors.Is(err, fs.ErrNotExist) {
		return file, err
	}
	if err := t.root.MkdirAll(filepath.FromSlash(path.Dir(rel)), dirPerm); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrDirFailed, rel, err)
	}
	return t.root.OpenFile(name, flag, filePerm)
}

func (t tree) rename(from, to string) error {
	if err := fsRename(t.root, filepath.FromSlash(from), filepath.FromSlash(to)); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrWriteFailed, to, err)
	}
	return nil
}

func (t tree) remove(rel string) error {
	err := fsRemove(t.root, filepath.FromSlash(rel))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s: %w", ErrWriteFailed, rel, err)
	}
	return nil
}

func (t tree) removeEmptyDirs(rel string, keep int) {
	for dir := path.Dir(rel); strings.Count(dir, "/") >= keep; dir = path.Dir(dir) {
		if t.root.Remove(filepath.FromSlash(dir)) != nil {
			return
		}
	}
}

func (t tree) walkLoose(dir string, visit func(Name)) error {
	pending := []string{dir}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries, err := t.readDir(current)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			child := current + "/" + entry.Name()
			if entry.IsDir() {
				pending = append(pending, child)
				continue
			}
			if strings.HasSuffix(child, lockSuffix) {
				continue
			}
			if name := Name(child); name.Validate() == nil {
				visit(name)
			}
		}
	}
	return nil
}

func (t tree) readDir(rel string) ([]fs.DirEntry, error) {
	file, err := t.root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, nil
	}
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrDirFailed, rel, err)
	}
	return entries, nil
}

func (t tree) clearEmptyTree(rel string) error {
	file, err := t.root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil
	}
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return fmt.Errorf("%w: %s holds %s", ErrNameConflict, rel, entry.Name())
		}
		if err := t.clearEmptyTree(rel + "/" + entry.Name()); err != nil {
			return err
		}
	}
	_ = t.root.Remove(filepath.FromSlash(rel))
	return nil
}

type lockFile struct {
	tree   tree
	target string
	file   *os.File
	done   bool
}

func newLock(t tree, target string) (*lockFile, error) {
	file, err := t.create(target+lockSuffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if errors.Is(err, fs.ErrExist) && !errors.Is(err, ErrDirFailed) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, target)
		}
		return nil, fmt.Errorf("%w: %s%s: %w", ErrWriteFailed, target, lockSuffix, err)
	}
	return &lockFile{tree: t, target: target, file: file}, nil
}

func (l *lockFile) write(data []byte) error {
	if _, err := fsWrite(l.file, data); err != nil {
		return fmt.Errorf("%w: %s%s: %w", ErrWriteFailed, l.target, lockSuffix, err)
	}
	return nil
}

func (l *lockFile) commit() error {
	_ = l.file.Close()
	if err := l.tree.rename(l.target+lockSuffix, l.target); err != nil {
		return err
	}
	l.done = true
	return nil
}

func (l *lockFile) release() {
	_ = l.file.Close()
	if l.done {
		return
	}
	_ = l.tree.remove(l.target + lockSuffix)
	l.done = true
}
