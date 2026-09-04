package app

import (
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/repos"
)

var isRepositoryPath = gitrepo.IsRepository

func (a *App) repoTreeState() map[string]repos.State {
	node, ok := a.registry.Active()
	if !ok || node.Kind == repo.KindGroup {
		return nil
	}
	if !isRepositoryPath(node.Path) {
		return map[string]repos.State{node.ID: {Missing: true}}
	}
	a.filesMu.Lock()
	modified := a.activeModified
	a.filesMu.Unlock()
	return map[string]repos.State{node.ID: {Modified: modified}}
}
