package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (w *Worktree) WorkingFile(path string) ([]byte, bool, error) {
	name := filepath.FromSlash(path)
	fi, err := fsLstatFile(w.root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := fsReadlinkFile(w.root, name)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, path, err)
		}
		return []byte(target), true, nil
	}
	if !fi.Mode().IsRegular() {
		return nil, false, nil
	}
	data, err := fsReadFileFile(w.root, name)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, path, err)
	}
	return w.convertForCheckin(path, data), true, nil
}

func (w *Worktree) HeadBlob(ctx context.Context, path string) (object.Mode, hash.ObjectID, error) {
	if err := ctx.Err(); err != nil {
		return 0, hash.Zero, err
	}
	_, _, headCommit, err := w.resolveHead()
	if err != nil {
		return 0, hash.Zero, err
	}
	if headCommit.IsZero() {
		return 0, hash.Zero, nil
	}
	commit, err := w.db.Commit(headCommit)
	if err != nil {
		return 0, hash.Zero, fmt.Errorf("%w: %w", ErrReadHead, err)
	}
	return w.treeLookup(ctx, commit.Tree, path)
}

func (w *Worktree) treeLookup(ctx context.Context, treeID hash.ObjectID, path string) (object.Mode, hash.ObjectID, error) {
	if err := ctx.Err(); err != nil {
		return 0, hash.Zero, err
	}
	name, rest, nested := strings.Cut(path, "/")
	tree, err := w.db.Tree(treeID)
	if err != nil {
		return 0, hash.Zero, fmt.Errorf("%w: %w", ErrReadHeadTree, err)
	}
	entry, ok := tree.Find(name)
	if !ok {
		return 0, hash.Zero, nil
	}
	if !nested {
		return entry.Mode, entry.ID, nil
	}
	if !entry.Mode.IsTree() {
		return 0, hash.Zero, nil
	}
	return w.treeLookup(ctx, entry.ID, rest)
}
