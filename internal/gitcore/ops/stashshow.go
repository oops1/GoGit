package ops

import (
	"cmp"
	"context"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type StashChanges struct {
	Entry     StashEntry
	Base      hash.ObjectID
	WorkTree  []diff.File
	Index     []diff.File
	Untracked []diff.File
}

func (c StashChanges) Files() []diff.File {
	files := slices.Concat(c.WorkTree, c.Untracked)
	slices.SortStableFunc(files, func(a, b diff.File) int {
		return cmp.Compare(cmp.Or(a.NewPath, a.OldPath), cmp.Or(b.NewPath, b.OldPath))
	})
	return files
}

func StashShow(ctx context.Context, r *repo.Repository, position int, opts diff.Options) (StashChanges, error) {
	if err := ctx.Err(); err != nil {
		return StashChanges{}, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return StashChanges{}, err
	}
	defer func() { _ = rc.close() }()
	parts, err := readStashParts(rc, position)
	if err != nil {
		return StashChanges{}, err
	}
	options, err := repoDiffOptions(r, opts)
	if err != nil {
		return StashChanges{}, err
	}
	store := mergeStore{db: rc.db}
	changes := StashChanges{Entry: parts.entry, Base: parts.stash.Parents[0]}
	if changes.WorkTree, err = diff.Trees(ctx, store, parts.base, parts.stash.Tree, options); err != nil {
		return StashChanges{}, err
	}
	if changes.Index, err = diff.Trees(ctx, store, parts.base, parts.index, options); err != nil {
		return StashChanges{}, err
	}
	if parts.untracked.IsZero() {
		return changes, nil
	}
	if changes.Untracked, err = diff.Trees(ctx, store, hash.Zero, parts.untracked, options); err != nil {
		return StashChanges{}, err
	}
	return changes, nil
}
