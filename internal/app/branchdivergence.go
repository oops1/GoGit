package app

import (
	"log/slog"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/branches"
)

var manyAheadBehind = repo.ManyAheadBehindIn

type branchDivergenceLoader struct {
	many func(revision.Context, []repo.BranchPair) (map[refs.Name]repo.Divergence, error)
	log  *slog.Logger
}

func (a *App) divergenceLoader() branchDivergenceLoader {
	return branchDivergenceLoader{many: manyAheadBehind, log: a.log}
}

func (a *App) statusDivergenceLoader() branchDivergenceLoader {
	return branchDivergenceLoader{many: repo.ManyAheadBehindIn, log: a.log}
}

func (a *App) clearBranchDivergence() {
	a.branchDivergenceGen.Add(1)
	a.branchesView.SetDivergence(nil)
}

func (a *App) refreshBranchDivergence(o *openedRepository, snap branches.Snapshot) {
	gen := a.branchDivergenceGen.Add(1)
	pairs := branches.DivergencePairs(snap)
	if len(pairs) == 0 {
		a.branchesView.SetDivergence(nil)
		return
	}
	loader := a.divergenceLoader()
	a.branchDivergenceWG.Go(func() {
		result := loader.load(o, pairs)
		a.Post(func() {
			if a.branchDivergenceGen.Load() == gen && a.opened() == o {
				a.branchesView.SetDivergence(result)
			}
		})
	})
}

func (l branchDivergenceLoader) counter(o *openedRepository) repo.Counter {
	return func(pairs []repo.BranchPair) (map[refs.Name]repo.Divergence, error) {
		return o.divergence.count(pairs, func(missing []repo.BranchPair) (map[refs.Name]repo.Divergence, error) {
			return l.many(o.revisionContext(), missing)
		})
	}
}

func (l branchDivergenceLoader) load(o *openedRepository, pairs []repo.BranchPair) map[refs.Name]repo.Divergence {
	result, err := l.counter(o)(pairs)
	if err != nil {
		l.log.Warn("compute branch divergence failed", "path", o.path, "error", err)
		return nil
	}
	o.divergence.keepOnly(pairs)
	return result
}
