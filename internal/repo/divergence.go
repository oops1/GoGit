package repo

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type Divergence struct {
	Ahead  int
	Behind int
}

var openDivergenceObjects = odb.Open

var openDivergenceRefs = refs.Open

func AheadBehind(r *gitrepo.Repository) (Divergence, bool, error) {
	store, err := openDivergenceRefs(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare()})
	if err != nil {
		return Divergence{}, false, err
	}
	defer func() { _ = store.Close() }()

	head, err := store.Lookup(refs.HEAD)
	if err != nil {
		return Divergence{}, false, err
	}
	if !head.IsSymbolic() {
		return Divergence{}, false, nil
	}

	branchCfg, ok := r.Config().Branch(head.SymbolicTarget.Short())
	if !ok || branchCfg.Remote == "" || len(branchCfg.Merge) == 0 {
		return Divergence{}, false, nil
	}

	trackingName := refs.RemoteBranchName(branchCfg.Remote, refs.Name(branchCfg.Merge[0]).Short())
	remoteCommit, found, err := lookupDivergenceTarget(store, trackingName)
	if err != nil {
		return Divergence{}, false, err
	}
	if !found {
		return Divergence{}, false, nil
	}

	localCommit, _, err := lookupDivergenceTarget(store, head.SymbolicTarget)
	if err != nil {
		return Divergence{}, false, err
	}

	shallow, err := r.Shallow()
	if err != nil {
		return Divergence{}, false, err
	}

	db, err := openDivergenceObjects(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return Divergence{}, false, err
	}
	defer func() { _ = db.Close() }()

	revCtx := revision.Context{Objects: db, Shallow: shallow}
	ahead, err := countDivergenceCommits(revCtx, localCommit, remoteCommit)
	if err != nil {
		return Divergence{}, false, err
	}
	behind, err := countDivergenceCommits(revCtx, remoteCommit, localCommit)
	if err != nil {
		return Divergence{}, false, err
	}
	return Divergence{Ahead: ahead, Behind: behind}, true, nil
}

func lookupDivergenceTarget(store *refs.Store, name refs.Name) (hash.ObjectID, bool, error) {
	ref, err := store.Lookup(name)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, false, nil
	}
	if err != nil {
		return hash.Zero, false, err
	}
	return ref.Target, true, nil
}

func countDivergenceCommits(ctx revision.Context, include, exclude hash.ObjectID) (int, error) {
	if include.IsZero() || include == exclude {
		return 0, nil
	}
	opts := revision.Options{Context: ctx, Include: []hash.ObjectID{include}}
	if !exclude.IsZero() {
		opts.Exclude = []hash.ObjectID{exclude}
	}
	count := 0
	for _, err := range revision.Walk(context.Background(), opts) {
		if err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}
