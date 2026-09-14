package ops

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	repackWindow            = 10
	repackDepth             = 50
	packFileExt             = ".pack"
	coreBigFileThresholdKey = "core.bigFileThreshold"
)

var (
	dbWritePack     = (*odb.DB).WritePack
	dbPackObjects   = (*odb.DB).PackObjects
	dbPutLoose      = (*odb.DB).PutLoose
	dbReloadPacks   = (*odb.DB).Reload
	removePackFiles = odb.RemovePackFiles
)

type RepackOptions struct {
	Expire time.Time
	Window int
	Depth  int
}

type RepackResult struct {
	Pack     string
	Objects  int
	Bytes    int64
	Removed  []string
	Busy     []string
	Loosened int
	Dropped  int
	Unpacked int
}

type repackPlan struct {
	ids       []hash.ObjectID
	reachable map[hash.ObjectID]struct{}
	loose     map[hash.ObjectID]struct{}
	inKeep    map[hash.ObjectID]struct{}
	old       []odb.PackInfo
	kept      *objectWalk
}

func Repack(ctx context.Context, r *repo.Repository, opts RepackOptions) (RepackResult, error) {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return RepackResult{}, err
	}
	defer func() { _ = db.Close() }()
	walk, err := reachableObjects(ctx, r, db)
	if err != nil {
		return RepackResult{}, err
	}
	return repackWith(ctx, r, db, walk, opts)
}

func repackWith(ctx context.Context, r *repo.Repository, db *odb.DB, walk *objectWalk, opts RepackOptions) (RepackResult, error) {
	threshold, err := bigFileThreshold(r)
	if err != nil {
		return RepackResult{}, err
	}
	plan, err := planRepack(walk, db, opts.Expire)
	if err != nil {
		return RepackResult{}, err
	}
	var result RepackResult
	if len(plan.ids) > 0 {
		written, err := dbWritePack(db, ctx, plan.ids, pack.WriteOptions{
			Window:           positiveOr(opts.Window, repackWindow),
			Depth:            positiveOr(opts.Depth, repackDepth),
			NameHashes:       walk.names,
			BigFileThreshold: threshold,
		})
		if err != nil {
			return RepackResult{}, err
		}
		result.Pack = strings.TrimSuffix(filepath.Base(written.PackPath), packFileExt)
		result.Objects = written.Objects
		result.Bytes = written.Bytes
	}
	if err := loosenUnreachable(db, plan, opts.Expire, &result); err != nil {
		return result, err
	}
	replaced := make([]string, 0, len(plan.old))
	for _, old := range plan.old {
		if old.Name != result.Pack {
			replaced = append(replaced, old.Name)
		}
	}
	_ = db.ForgetPacks(replaced...)
	for _, name := range replaced {
		if err := removePackFiles(r.PackDir(), name); err != nil {
			result.Busy = append(result.Busy, name)
			continue
		}
		result.Removed = append(result.Removed, name)
	}
	if _, err := dbReloadPacks(db); err != nil {
		return result, err
	}
	unpacked, err := removePackedLoose(ctx, db)
	result.Unpacked = unpacked
	return result, err
}

func bigFileThreshold(r *repo.Repository) (int64, error) {
	if !r.Config().Has(coreBigFileThresholdKey) {
		return pack.DefaultBigFileThreshold, nil
	}
	return r.Config().GetInt(coreBigFileThresholdKey)
}

func planRepack(walk *objectWalk, db *odb.DB, expire time.Time) (repackPlan, error) {
	plan := repackPlan{
		reachable: maps.Clone(walk.seen),
		loose:     make(map[hash.ObjectID]struct{}),
		inKeep:    make(map[hash.ObjectID]struct{}),
		kept:      walk,
	}
	if !expire.IsZero() {
		if err := extendWithRecent(walk, db, expire); err != nil {
			return repackPlan{}, err
		}
	}
	packs, err := dbPacks(db)
	if err != nil {
		return repackPlan{}, err
	}
	candidates := make(map[hash.ObjectID]struct{})
	for _, p := range packs {
		if p.Keep {
			for id := range dbPackObjects(db, p.Name) {
				plan.inKeep[id] = struct{}{}
			}
			continue
		}
		plan.old = append(plan.old, p)
		for id := range dbPackObjects(db, p.Name) {
			candidates[id] = struct{}{}
		}
	}
	for loose, err := range dbLooseObjects(db) {
		if err != nil {
			return repackPlan{}, err
		}
		plan.loose[loose.ID] = struct{}{}
		candidates[loose.ID] = struct{}{}
	}
	for id := range candidates {
		if _, reachable := plan.reachable[id]; !reachable {
			continue
		}
		if _, kept := plan.inKeep[id]; kept {
			continue
		}
		plan.ids = append(plan.ids, id)
	}
	slices.SortFunc(plan.ids, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return plan, nil
}

func loosenUnreachable(db *odb.DB, plan repackPlan, expire time.Time, result *RepackResult) error {
	done := make(map[hash.ObjectID]struct{})
	for _, old := range plan.old {
		for id := range dbPackObjects(db, old.Name) {
			if plan.stays(id, done) {
				continue
			}
			done[id] = struct{}{}
			if !expire.IsZero() && old.ModTime.Before(expire) && !plan.kept.reachable(id) {
				result.Dropped++
				continue
			}
			kind, data, err := dbGet(db, id)
			if err != nil {
				return err
			}
			if _, err := dbPutLoose(db, kind, data, old.ModTime); err != nil {
				return err
			}
			result.Loosened++
		}
	}
	return nil
}

func (p repackPlan) stays(id hash.ObjectID, done map[hash.ObjectID]struct{}) bool {
	for _, set := range []map[hash.ObjectID]struct{}{p.reachable, p.inKeep, p.loose, done} {
		if _, ok := set[id]; ok {
			return true
		}
	}
	return false
}

func removePackedLoose(ctx context.Context, db *odb.DB) (int, error) {
	removed := 0
	for loose, err := range dbLooseObjects(db) {
		if err != nil {
			return removed, err
		}
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		packed, err := dbPacked(db, loose.ID)
		if err != nil {
			return removed, err
		}
		if !packed {
			continue
		}
		if err := dbRemoveLoose(db, loose.ID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, dbRemoveEmptyFanouts(db)
}

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
