package ops

import (
	"context"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var (
	dbRemoveLoose        = (*odb.DB).RemoveLoose
	dbRemoveTemp         = (*odb.DB).RemoveTemp
	dbRemoveEmptyFanouts = (*odb.DB).RemoveEmptyFanouts
	dbPackedSince        = (*odb.DB).PackedSince
	pruneNow             = time.Now
)

type PruneOptions struct {
	Expire time.Time
	DryRun bool
}

type PruneResult struct {
	Unreachable      []hash.ObjectID
	UnreachableBytes int64
	Packed           int
	PackedBytes      int64
	Temps            int
	TempBytes        int64
}

func Prune(ctx context.Context, r *repo.Repository, opts PruneOptions) (PruneResult, error) {
	expire := opts.Expire
	if expire.IsZero() {
		expire = pruneNow()
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return PruneResult{}, err
	}
	defer func() { _ = db.Close() }()
	walk, err := keptObjects(ctx, r, db, expire)
	if err != nil {
		return PruneResult{}, err
	}
	var result PruneResult
	if err := pruneLoose(ctx, db, walk, opts.DryRun, &result); err != nil {
		return result, err
	}
	if err := pruneTemps(db, expire, opts.DryRun, &result); err != nil {
		return result, err
	}
	if opts.DryRun {
		return result, nil
	}
	return result, dbRemoveEmptyFanouts(db)
}

func keptObjects(ctx context.Context, r *repo.Repository, db *odb.DB, expire time.Time) (*objectWalk, error) {
	walk, err := reachableObjects(ctx, r, db)
	if err != nil {
		return nil, err
	}
	for loose, err := range dbLooseObjects(db) {
		if err != nil {
			return nil, err
		}
		if !loose.ModTime.Before(expire) {
			walk.push(loose.ID, 0)
		}
	}
	for id := range dbPackedSince(db, expire) {
		walk.push(id, 0)
	}
	walk.broken = func(hash.ObjectID, error) error { return nil }
	if err := walk.run(); err != nil {
		return nil, err
	}
	return walk, nil
}

func pruneLoose(ctx context.Context, db *odb.DB, walk *objectWalk, dryRun bool, result *PruneResult) error {
	for loose, err := range dbLooseObjects(db) {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		packed, err := dbPacked(db, loose.ID)
		if err != nil {
			return err
		}
		switch {
		case packed:
			result.Packed++
			result.PackedBytes += loose.Size
		case !walk.reachable(loose.ID):
			result.Unreachable = append(result.Unreachable, loose.ID)
			result.UnreachableBytes += loose.Size
		default:
			continue
		}
		if dryRun {
			continue
		}
		if err := dbRemoveLoose(db, loose.ID); err != nil {
			return err
		}
	}
	return nil
}

func pruneTemps(db *odb.DB, expire time.Time, dryRun bool, result *PruneResult) error {
	temps, err := dbTempFiles(db)
	if err != nil {
		return err
	}
	for _, temp := range temps {
		if !temp.ModTime.Before(expire) {
			continue
		}
		result.Temps++
		result.TempBytes += temp.Size
		if dryRun {
			continue
		}
		if err := dbRemoveTemp(db, temp.Path); err != nil {
			return err
		}
	}
	return nil
}
