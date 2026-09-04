package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func validateBranchName(name string) error {
	if err := refs.BranchName(name).Validate(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidBranchName, name, err)
	}
	return nil
}

func currentBranchRef(store *refs.Store) (refs.Name, error) {
	ref, err := store.Lookup(refs.HEAD)
	if err != nil {
		return "", err
	}
	if !ref.IsSymbolic() {
		return "", nil
	}
	return ref.SymbolicTarget, nil
}

func CreateBranch(ctx context.Context, r *repo.Repository, name string, startPoint hash.ObjectID, opts CreateBranchOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBranchName(name); err != nil {
		return err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	if err := rc.requireIdentity(); err != nil {
		return err
	}

	ref := refs.BranchName(name)
	tx := rc.refs.Begin()
	tx.SetMessage("branch: Created from " + startPoint.String())
	var txErr error
	if opts.Force {
		txErr = tx.Set(ref, startPoint)
	} else {
		txErr = tx.Update(ref, startPoint, hash.Zero)
	}
	if txErr != nil {
		tx.Rollback()
		return txErr
	}
	if err := tx.Commit(); err != nil {
		if errors.Is(err, refs.ErrOldValueMismatch) {
			return fmt.Errorf("%w: %s", ErrBranchExists, name)
		}
		return err
	}
	return nil
}

func DeleteBranch(ctx context.Context, r *repo.Repository, name string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBranchName(name); err != nil {
		return err
	}
	ref := refs.BranchName(name)

	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()

	current, err := currentBranchRef(rc.refs)
	if err != nil {
		return err
	}
	if current == ref {
		return fmt.Errorf("%w: %s", ErrBranchCheckedOut, name)
	}

	target, err := rc.refs.Lookup(ref)
	if errors.Is(err, refs.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrBranchNotFound, name)
	}
	if err != nil {
		return err
	}

	if !force {
		merged, err := isMergedIntoHead(rc, target.Target)
		if err != nil {
			return err
		}
		if !merged {
			return fmt.Errorf("%w: %s", ErrBranchNotMerged, name)
		}
	}

	tx := rc.refs.Begin()
	if err := txDelete(tx, ref, target.Target); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func isMergedIntoHead(rc *repoContext, commit hash.ObjectID) (bool, error) {
	head, err := resolveHeadCommit(rc.refs)
	if err != nil {
		return false, err
	}
	if head.IsZero() {
		return commit.IsZero(), nil
	}
	return revision.IsAncestor(revision.Context{Objects: rc.db}, commit, head)
}

func RenameBranch(ctx context.Context, r *repo.Repository, from, to string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBranchName(from); err != nil {
		return err
	}
	if err := validateBranchName(to); err != nil {
		return err
	}
	fromRef, toRef := refs.BranchName(from), refs.BranchName(to)

	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	if err := rc.requireIdentity(); err != nil {
		return err
	}

	target, err := rc.refs.Lookup(fromRef)
	if errors.Is(err, refs.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrBranchNotFound, from)
	}
	if err != nil {
		return err
	}
	current, err := currentBranchRef(rc.refs)
	if err != nil {
		return err
	}

	tx := rc.refs.Begin()
	tx.SetMessage("branch: renamed " + from + " to " + to)
	var setErr error
	if force {
		setErr = tx.Set(toRef, target.Target)
	} else {
		setErr = tx.Update(toRef, target.Target, hash.Zero)
	}
	if setErr != nil {
		tx.Rollback()
		return setErr
	}
	if err := tx.Delete(fromRef, target.Target); err != nil {
		tx.Rollback()
		return err
	}
	if current == fromRef {
		if err := txSetSymbolic(tx, refs.HEAD, toRef); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		if errors.Is(err, refs.ErrOldValueMismatch) {
			return fmt.Errorf("%w: %s", ErrBranchExists, to)
		}
		return err
	}
	return nil
}
