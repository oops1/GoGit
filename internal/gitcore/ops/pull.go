package ops

import (
	"context"
	"errors"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type PullOptions struct {
	Remote   string
	Fetch    remote.FetchOptions
	Progress progress.Func
}

type PullResult struct {
	Fetch    remote.FetchResult
	Old      hash.ObjectID
	New      hash.ObjectID
	Updated  bool
	UpToDate bool
}

func pullTrackingRef(r *repo.Repository, name refs.Name) (hash.ObjectID, bool, error) {
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare()})
	if err != nil {
		return hash.Zero, false, err
	}
	defer func() { _ = store.Close() }()
	ref, err := store.Lookup(name)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, false, nil
	}
	if err != nil {
		return hash.Zero, false, err
	}
	return ref.Target, true, nil
}

func isFastForwardCommit(r *repo.Repository, old, newCommit hash.ObjectID) (bool, error) {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	return revision.IsAncestor(revision.Context{Objects: db}, old, newCommit)
}

func commitPulledBranch(r *repo.Repository, branch refs.Name, oldCommit, newCommit hash.ObjectID) error {
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	tx := rc.refs.Begin()
	tx.SetMessage("pull: Fast-forward")
	if err := txUpdate(tx, branch, newCommit, oldCommit); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func Pull(ctx context.Context, r *repo.Repository, opts PullOptions) (PullResult, error) {
	if err := ctx.Err(); err != nil {
		return PullResult{}, err
	}

	branchShort, onBranch, err := currentBranchShort(r)
	if err != nil {
		return PullResult{}, err
	}
	if !onBranch {
		return PullResult{}, ErrDetachedHead
	}

	cfg := r.Config()
	branchCfg, _ := cfg.Branch(branchShort)
	remoteName := opts.Remote
	if remoteName == "" {
		remoteName = branchCfg.Remote
	}
	if remoteName == "" || len(branchCfg.Merge) == 0 {
		return PullResult{}, ErrNoUpstream
	}
	mergeRef := refs.Name(branchCfg.Merge[0])

	rem, err := remote.Load(cfg, remoteName)
	if err != nil {
		return PullResult{}, err
	}

	fetchResult, err := fetchRemote(ctx, r, rem, opts.Fetch)
	if err != nil {
		return PullResult{}, err
	}

	branchRef := refs.BranchName(branchShort)
	oldCommit, _, err := pullTrackingRef(r, branchRef)
	if err != nil {
		return PullResult{Fetch: fetchResult}, err
	}

	trackingName := refs.RemoteBranchName(remoteName, mergeRef.Short())
	newCommit, found, err := pullTrackingRef(r, trackingName)
	if err != nil {
		return PullResult{Fetch: fetchResult}, err
	}
	if !found {
		return PullResult{Fetch: fetchResult}, ErrNoUpstream
	}

	if oldCommit == newCommit {
		return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit, UpToDate: true}, nil
	}

	if !oldCommit.IsZero() {
		ancestor, err := isFastForwardCommit(r, oldCommit, newCommit)
		if err != nil {
			return PullResult{Fetch: fetchResult}, err
		}
		if !ancestor {
			return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit}, ErrNotFastForward
		}
	}

	if _, err := identityOf(r, time.Now()); err != nil {
		return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit}, err
	}

	if !r.IsBare() {
		if err := CheckoutTree(ctx, r, newCommit, CheckoutOptions{Progress: opts.Progress}); err != nil {
			var overwrite *OverwriteError
			if errors.As(err, &overwrite) {
				return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit}, ErrDirtyWorkTree
			}
			return PullResult{Fetch: fetchResult}, err
		}
	}

	if err := commitPulledBranch(r, branchRef, oldCommit, newCommit); err != nil {
		return PullResult{Fetch: fetchResult}, err
	}

	return PullResult{Fetch: fetchResult, Old: oldCommit, New: newCommit, Updated: true}, nil
}
