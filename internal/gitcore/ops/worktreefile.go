package ops

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const gitDirName = ".git"

func holdsRepository(entries []fs.DirEntry) bool {
	return slices.ContainsFunc(entries, func(entry fs.DirEntry) bool { return entry.Name() == gitDirName })
}

func writeWorktreeBlob(wt *workingTree, rel string, mode object.Mode, data []byte) error {
	if err := prepareWorktreePath(wt, rel, mode); err != nil {
		return err
	}
	name := filepath.FromSlash(rel)
	if mode.IsSymlink() && wt.symlinks {
		_ = fsRootRemove(wt.root, name)
		return fsRootSymlink(wt.root, string(data), name)
	}
	return writeRegularFile(wt, name, mode, func(file io.Writer) error {
		_, err := file.Write(data)
		return err
	})
}

func writeWorktreeStream(wt *workingTree, rel string, mode object.Mode, write func(io.Writer) error) error {
	if err := prepareWorktreePath(wt, rel, mode); err != nil {
		return err
	}
	return writeRegularFile(wt, filepath.FromSlash(rel), mode, write)
}

func prepareWorktreePath(wt *workingTree, rel string, mode object.Mode) error {
	if err := index.VerifyPath(rel, mode, writeGuardRules); err != nil {
		return err
	}
	if dir := parentOf(rel); dir != "" {
		return ensureDirectories(wt.root, dir)
	}
	return nil
}

func writeRegularFile(wt *workingTree, name string, mode object.Mode, write func(io.Writer) error) error {
	if info, err := fsRootLstat(wt.root, name); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := fsRootRemove(wt.root, name); err != nil {
			return err
		}
	}
	perm := fs.FileMode(0o666)
	if mode == object.ModeExecutable {
		perm = 0o777
	}
	file, err := fsRootOpenFile(wt.root, name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if err := write(file); err != nil {
		_ = file.Close()
		return err
	}
	return errors.Join(file.Close(), fixExecutable(wt, name, mode == object.ModeExecutable))
}

func fixExecutable(wt *workingTree, name string, executable bool) error {
	if !wt.fileMode {
		return nil
	}
	info, err := fsRootLstat(wt.root, name)
	if err != nil {
		return err
	}
	current := info.Mode().Perm()
	wanted := current &^ 0o111
	if executable {
		wanted |= current & 0o444 >> 2
	}
	if wanted == current {
		return nil
	}
	return fsRootChmod(wt.root, name, wanted)
}
