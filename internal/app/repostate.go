package app

import (
	"maps"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/repos"
)

var isRepositoryPath = gitrepo.IsRepository

var currentBranch = repo.CurrentBranch

type branchNode struct {
	id   string
	path string
}

func (a *App) branchNodes() []branchNode {
	var nodes []branchNode
	for n := range a.registry.Walk() {
		if n.Kind == repo.KindGroup {
			continue
		}
		nodes = append(nodes, branchNode{id: n.ID, path: n.Path})
	}
	return nodes
}

func readBranches(nodes []branchNode) map[string]string {
	branchesByID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		branchesByID[n.id] = currentBranch(n.path)
	}
	return branchesByID
}

func (a *App) beginBranchRebuild() uint64 {
	a.branchMu.Lock()
	defer a.branchMu.Unlock()
	a.branchGen++
	a.branchFresh = map[string]string{}
	return a.branchGen
}

func (a *App) loadBranchCache() {
	next := readBranches(a.branchNodes())
	a.installBranchCache(a.beginBranchRebuild(), next)
}

func (a *App) refreshBranchCache() {
	nodes := a.branchNodes()
	gen := a.beginBranchRebuild()
	a.branchWG.Go(func() {
		next := readBranches(nodes)
		a.Post(func() {
			if a.installBranchCache(gen, next) {
				a.reposView.Render(a.registry, a.repoTreeState())
			}
		})
	})
}

func (a *App) installBranchCache(gen uint64, next map[string]string) bool {
	a.branchMu.Lock()
	defer a.branchMu.Unlock()
	if gen != a.branchGen {
		return false
	}
	maps.Copy(next, a.branchFresh)
	if maps.Equal(next, a.branchCache) {
		return false
	}
	a.branchCache = next
	return true
}

func (a *App) setActiveBranch(id string, snap branches.Snapshot) {
	branch := snap.Current
	if snap.Detached {
		branch = shortHash(snap.HeadID)
	}
	a.branchMu.Lock()
	defer a.branchMu.Unlock()
	next := maps.Clone(a.branchCache)
	next[id] = branch
	a.branchCache = next
	a.branchFresh[id] = branch
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
	div := a.getDivergence()
	s.HasUpstream = div.HasUpstream
	s.Ahead = div.Ahead
	s.Behind = div.Behind
	state[node.ID] = s
	return state
}

func (a *App) onRepoTreeSelect(id string) {
	a.setSelected(id)
	a.leaveCommitView()
	a.clearFilesDirFilter()
}

func (a *App) onRepoTreeSelectDirectory(_, relPath string) {
	a.leaveCommitView()
	a.setFilesDirFilter(relPath)
}

func (a *App) leaveCommitView() {
	if !a.commitIsSelected() {
		return
	}
	a.stopDiff()
	a.setSelectedCommit(hash.ObjectID{})
	a.setCommitSelected(false)
	a.journalView.ClearSelection()
	a.clearDiff()
	a.requestWorking()
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
