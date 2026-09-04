package worktree

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/object"
)

const gitDirName = ".git"

var (
	fsOpenDir  = func(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
	fsReadDir  = func(file *os.File) ([]fs.DirEntry, error) { return file.ReadDir(-1) }
	fsCloseDir = func(file *os.File) error { return file.Close() }
)

type entryKind uint8

const (
	kindRegular entryKind = iota
	kindSymlink
	kindSubmodule
)

func kindOfMode(mode object.Mode) entryKind {
	switch {
	case mode.IsSymlink():
		return kindSymlink
	case mode.IsSubmodule():
		return kindSubmodule
	default:
		return kindRegular
	}
}

func kindOfInfo(fi os.FileInfo) entryKind {
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return kindSymlink
	case fi.IsDir():
		return kindSubmodule
	default:
		return kindRegular
	}
}

func joinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func (w *Worktree) readDir(rel string) ([]fs.DirEntry, error) {
	name := "."
	if rel != "" {
		name = filepath.FromSlash(rel)
	}
	file, err := fsOpenDir(w.root, name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, rel, err)
	}
	entries, err := fsReadDir(file)
	closeErr := fsCloseDir(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, rel, err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, rel, closeErr)
	}
	return entries, nil
}
