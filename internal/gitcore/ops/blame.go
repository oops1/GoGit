package ops

import (
	"context"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/blame"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type BlameOptions struct {
	Follow bool
	Diff   diff.Options
}

func Blame(ctx context.Context, r *repo.Repository, rev, path string, opts BlameOptions) (blame.Result, error) {
	if err := ctx.Err(); err != nil {
		return blame.Result{}, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return blame.Result{}, err
	}
	defer func() { _ = rc.close() }()

	clean, err := cleanRepoPath(path)
	if err != nil {
		return blame.Result{}, err
	}
	commit, err := resolveCommittish(rc, rev)
	if err != nil {
		return blame.Result{}, err
	}
	return blame.File(ctx, mergeStore{db: rc.db}, commit, clean, blame.Options{Diff: opts.Diff, FollowRenames: opts.Follow})
}

func resolveCommittish(rc *repoContext, rev string) (hash.ObjectID, error) {
	if rev == "" {
		rev = oursLabel
	}
	parsed, err := revision.Parse(rev, revision.Context{Objects: rc.db, Refs: rc.refs, Head: refs.HEAD})
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %s: %w", ErrTargetNotFound, rev, err)
	}
	kind, commit, err := dbPeel(rc.db, parsed.ID)
	if err != nil {
		return hash.Zero, err
	}
	if kind != object.TypeCommit {
		return hash.Zero, fmt.Errorf("%w: %s is a %s, not a commit", ErrTargetNotFound, rev, kind)
	}
	return commit, nil
}
