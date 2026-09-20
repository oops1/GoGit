package app

import (
	"log/slog"

	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/branches"
)

var manyAheadBehind = repo.ManyAheadBehind

type branchDivergenceLoader struct {
	open func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error)
	many func(*gitrepo.Repository, []repo.BranchPair) (map[refs.Name]repo.Divergence, error)
	log  *slog.Logger
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
	loader := branchDivergenceLoader{open: openGitRepository, many: manyAheadBehind, log: a.log}
	a.branchDivergenceWG.Go(func() {
		result := loader.load(o.path, pairs)
		a.Post(func() {
			if a.branchDivergenceGen.Load() == gen && a.opened() == o {
				a.branchesView.SetDivergence(result)
			}
		})
	})
}

func (l branchDivergenceLoader) load(path string, pairs []repo.BranchPair) map[refs.Name]repo.Divergence {
	r, err := l.open(path, gitrepo.OpenOptions{})
	if err != nil {
		l.log.Warn("open repository for branch divergence failed", "path", path, "error", err)
		return nil
	}
	defer func() { _ = r.Close() }()
	result, err := l.many(r, pairs)
	if err != nil {
		l.log.Warn("compute branch divergence failed", "path", path, "error", err)
		return nil
	}
	return result
}
