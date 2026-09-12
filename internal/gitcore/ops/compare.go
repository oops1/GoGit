package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type CompareOptions struct {
	Diff diff.Options
}

type CompareResult struct {
	Left    hash.ObjectID
	Right   hash.ObjectID
	Base    hash.ObjectID
	Ahead   int
	Behind  int
	Changes []diff.File
}

func (r CompareResult) Same() bool { return r.Left == r.Right }

func Compare(ctx context.Context, r *repo.Repository, left, right string, opts CompareOptions) (CompareResult, error) {
	if err := ctx.Err(); err != nil {
		return CompareResult{}, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return CompareResult{}, err
	}
	defer func() { _ = rc.close() }()

	result := CompareResult{}
	if result.Left, err = resolveCommittish(rc, left); err != nil {
		return CompareResult{}, err
	}
	if result.Right, err = resolveCommittish(rc, right); err != nil {
		return CompareResult{}, err
	}
	store := mergeStore{db: rc.db}
	bases, err := revision.MergeBase(revision.Context{Objects: store}, result.Left, result.Right)
	if err != nil {
		return CompareResult{}, err
	}
	if len(bases) > 0 {
		result.Base = bases[0]
	}
	if result.Behind, err = countCommits(ctx, store, result.Left, result.Right); err != nil {
		return CompareResult{}, err
	}
	if result.Ahead, err = countCommits(ctx, store, result.Right, result.Left); err != nil {
		return CompareResult{}, err
	}
	leftTree, err := treeOfCommit(rc, result.Left)
	if err != nil {
		return CompareResult{}, err
	}
	rightTree, err := treeOfCommit(rc, result.Right)
	if err != nil {
		return CompareResult{}, err
	}
	options := opts.Diff
	if options.RenameThreshold == 0 {
		options = diff.Defaults()
	}
	if result.Changes, err = diff.Trees(ctx, store, leftTree, rightTree, options); err != nil {
		return CompareResult{}, err
	}
	return result, nil
}

func countCommits(ctx context.Context, store mergeStore, exclude, include hash.ObjectID) (int, error) {
	count := 0
	walk := revision.Walk(ctx, revision.Options{
		Context: revision.Context{Objects: store},
		Include: []hash.ObjectID{include},
		Exclude: []hash.ObjectID{exclude},
	})
	for _, err := range walk {
		if err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

func treeOfCommit(rc *repoContext, commit hash.ObjectID) (hash.ObjectID, error) {
	parsed, err := dbCommit(rc.db, commit)
	if err != nil {
		return hash.Zero, err
	}
	return parsed.Tree, nil
}
