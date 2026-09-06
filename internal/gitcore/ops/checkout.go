package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type CheckoutOptions struct {
	Force    bool
	Progress progress.Func
}

func CheckoutTree(ctx context.Context, r *repo.Repository, commit hash.ObjectID, opts CheckoutOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()

	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	targetTree, err := commitTreeEntries(db, commit)
	if err != nil {
		return err
	}

	opts.Progress.Phase(progress.PhaseCheckout)
	return layoutWorkingTree(ctx, r, wt, db, targetTree, opts.Force)
}
