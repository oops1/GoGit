package ops

import (
	"context"
	"time"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	defaultPruneExpire    = "2.weeks.ago"
	gcPruneExpireKey      = "gc.pruneExpire"
	gcWriteCommitGraphKey = "gc.writeCommitGraph"
)

var (
	gcNow         = time.Now
	storePackRefs = (*refs.Store).PackRefs
)

type GCResult struct {
	Repack      RepackResult
	Prune       PruneResult
	Pruned      bool
	CommitGraph int
}

func GC(ctx context.Context, r *repo.Repository) (GCResult, error) {
	expire, expires, err := gcExpiry(r)
	if err != nil {
		return GCResult{}, err
	}
	writeGraph, err := gcWritesCommitGraph(r)
	if err != nil {
		return GCResult{}, err
	}
	if err := packRefs(r); err != nil {
		return GCResult{}, err
	}
	var result GCResult
	repackOpts := RepackOptions{}
	if expires {
		repackOpts.Expire = expire
	}
	if result.Repack, err = Repack(ctx, r, repackOpts); err != nil {
		return result, err
	}
	if expires {
		if result.Prune, err = Prune(ctx, r, PruneOptions{Expire: expire}); err != nil {
			return result, err
		}
		result.Pruned = true
	}
	if writeGraph {
		result.CommitGraph, err = WriteCommitGraph(ctx, r)
	}
	return result, err
}

func gcExpiry(r *repo.Repository) (time.Time, bool, error) {
	value, ok := r.Config().Get(gcPruneExpireKey)
	if !ok {
		value = defaultPruneExpire
	}
	return parseExpiry(value, gcNow())
}

func gcWritesCommitGraph(r *repo.Repository) (bool, error) {
	if !r.Config().Has(gcWriteCommitGraphKey) {
		return true, nil
	}
	return r.Config().GetBool(gcWriteCommitGraphKey)
}

func packRefs(r *repo.Repository) error {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare(), Peeler: db})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return storePackRefs(store, true)
}
