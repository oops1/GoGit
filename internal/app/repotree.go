package app

import (
	"slices"

	"github.com/oops1/gogit/internal/repo"
)

func (a *App) moveTreeNode(id, targetID string, inside bool) {
	node, ok := a.registry.Find(id)
	if !ok {
		return
	}
	parentID, ok := a.moveTargetGroup(targetID, inside)
	if !ok || parentID == id {
		return
	}
	if err := a.reparent(node, parentID); err != nil {
		a.log.Warn("move tree node failed", "id", id, "parent", parentID, "error", err)
		return
	}
	a.saveRepositoryTree()
}

func (a *App) moveTargetGroup(targetID string, inside bool) (string, bool) {
	if inside {
		return targetID, true
	}
	parent, ok := a.registry.ParentOf(targetID)
	if !ok {
		return "", true
	}
	if parent.Kind != repo.KindGroup {
		return "", false
	}
	return parent.ID, true
}

func (a *App) reparent(node *repo.Node, parentID string) error {
	if node.Kind == repo.KindGroup {
		return a.registry.MoveGroup(node.ID, parentID)
	}
	return a.registry.MoveRepository(node.ID, parentID)
}

func (a *App) saveRepositoryTree() {
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
	a.reposView.Render(a.registry, a.repoTreeState())
}

func (a *App) rememberGroupState(id string, expanded bool) {
	at := slices.Index(a.cfg.UI.CollapsedGroups, id)
	switch {
	case expanded && at >= 0:
		a.cfg.UI.CollapsedGroups = slices.Delete(a.cfg.UI.CollapsedGroups, at, at+1)
	case !expanded && at < 0:
		a.cfg.UI.CollapsedGroups = append(a.cfg.UI.CollapsedGroups, id)
	default:
		return
	}
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}
