package app

import (
	"strings"

	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
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
	muted := a.mutedDirs
	a.filesMu.Unlock()
	s.Modified = modified
	s.MutedDirs = muted
	state[node.ID] = s
	return state
}

func (a *App) onRepoTreeSelect(id string) {
	a.setSelected(id)
	a.clearFilesDirFilter()
}

func (a *App) onRepoTreeSelectDirectory(_, relPath string) {
	a.setFilesDirFilter(relPath)
}

func mutedDirectories(entries []worktree.Entry) []string {
	var dirs []string
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		if e.Unstaged != worktree.StatusIgnored && e.Unstaged != worktree.StatusUntracked {
			continue
		}
		dirs = append(dirs, strings.TrimSuffix(e.Path, "/"))
	}
	return dirs
}
