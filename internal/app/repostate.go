package app

import (
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/repos"
)

var isRepositoryPath = gitrepo.IsRepository

var currentBranch = repo.CurrentBranch

func (a *App) refreshBranchCache() {
	next := make(map[string]string)
	for n := range a.registry.Walk() {
		if n.Kind == repo.KindGroup {
			continue
		}
		next[n.ID] = currentBranch(n.Path)
	}
	a.branchMu.Lock()
	a.branchCache = next
	a.branchMu.Unlock()
}

func (a *App) repoTreeState() map[string]repos.State {
	a.branchMu.Lock()
	cache := a.branchCache
	a.branchMu.Unlock()

	state := make(map[string]repos.State, len(cache))
	for id, branch := range cache {
		state[id] = repos.State{Branch: branch}
	}

	node, ok := a.registry.Active()
	if !ok || node.Kind == repo.KindGroup {
		return state
	}
	s := state[node.ID]
	if !isRepositoryPath(node.Path) {
		s.Missing = true
		state[node.ID] = s
		return state
	}
	a.filesMu.Lock()
	modified := a.activeModified
	a.filesMu.Unlock()
	s.Modified = modified
	state[node.ID] = s
	return state
}

func (a *App) onRepoTreeSelect(id string) {
	a.setSelected(id)
	a.clearFilesDirFilter()
}

func (a *App) onRepoTreeSelectDirectory(repoID, relPath string) {
	if active, ok := a.registry.Active(); !ok || active.ID != repoID {
		a.ActivateRepository(repoID)
	}
	a.setFilesDirFilter(relPath)
}
