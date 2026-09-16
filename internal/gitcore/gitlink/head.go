package gitlink

import (
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var ErrNoCommit = errors.New("gitlink: the nested repository has no commit checked out")

func Head(layout repo.Layout) (hash.ObjectID, error) {
	store, err := refs.Open(refs.Options{GitDir: layout.GitDir, CommonDir: layout.CommonDir})
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %w", ErrNoCommit, err)
	}
	defer func() { _ = store.Close() }()
	ref, err := store.Resolve(refs.HEAD)
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %w", ErrNoCommit, err)
	}
	return ref.Target, nil
}

func HeadOf(dir string) (hash.ObjectID, bool, error) {
	layout, ok := repo.DiscoverWorkTree(dir)
	if !ok {
		return hash.Zero, false, nil
	}
	id, err := Head(layout)
	return id, true, err
}
