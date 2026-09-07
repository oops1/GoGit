package app

import (
	"errors"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
)

var listWorktrees = ops.ListWorktrees

func (a *App) syncWorktrees(parentID string, r *gitrepo.Repository) bool {
	list, err := listWorktrees(r)
	if err != nil {
		a.log.Warn("list worktrees failed", "error", err)
		return false
	}
	onDisk := map[string]ops.Worktree{}
	for _, wt := range list {
		if wt.Main {
			continue
		}
		onDisk[normalisedWorktreePath(wt.Path)] = wt
	}
	changed := a.dropVanishedWorktrees(parentID, onDisk)
	return a.registerNewWorktrees(parentID, onDisk) || changed
}

func (a *App) dropVanishedWorktrees(parentID string, onDisk map[string]ops.Worktree) bool {
	changed := false
	for _, node := range a.worktreeNodes(parentID) {
		if _, ok := onDisk[normalisedWorktreePath(node.Path)]; ok {
			continue
		}
		if err := a.registry.RemoveRepository(node.ID); err != nil {
			a.log.Warn("forget worktree failed", "path", node.Path, "error", err)
			continue
		}
		changed = true
	}
	return changed
}

func (a *App) registerNewWorktrees(parentID string, onDisk map[string]ops.Worktree) bool {
	changed := false
	for path, wt := range onDisk {
		if _, ok := a.registry.FindByPath(path); ok {
			continue
		}
		if _, err := a.registry.AddWorktree(parentID, worktreeNodeName(wt), wt.Path); err != nil {
			if !errors.Is(err, repo.ErrDuplicatePath) {
				a.log.Warn("add worktree failed", "path", wt.Path, "error", err)
			}
			continue
		}
		changed = true
	}
	return changed
}

func (a *App) worktreeNodes(parentID string) []*repo.Node {
	parent, ok := a.registry.Find(parentID)
	if !ok {
		return nil
	}
	out := make([]*repo.Node, 0, len(parent.Children))
	for _, child := range parent.Children {
		if child.Kind == repo.KindWorktree {
			out = append(out, child)
		}
	}
	return out
}

func worktreeNodeName(wt ops.Worktree) string {
	if wt.Branch != "" {
		return wt.Branch.Short()
	}
	return filepath.Base(wt.Path)
}

func normalisedWorktreePath(path string) string {
	return filepath.Clean(path)
}

func (a *App) adoptWorktreesOf(node *repo.Node, opened *openedRepository) {
	if node.Kind != repo.KindRepository {
		return
	}
	if !a.syncWorktrees(node.ID, opened.repo) {
		return
	}
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}
