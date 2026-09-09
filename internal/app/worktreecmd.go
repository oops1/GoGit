package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/worktree"
)

var newWorktreeView = worktree.NewView

var addWorktree = ops.AddWorktree

var removeWorktree = ops.RemoveWorktree

var pruneWorktrees = ops.PruneWorktrees

func (a *App) registerWorktreeHandlers() {
	a.handlers[CmdAddWorktree] = a.openAddWorktree
	a.handlers[CmdRemoveWorktree] = a.removeActiveWorktree
	a.handlers[CmdPruneWorktrees] = a.pruneObsoleteWorktrees
}

func (a *App) openAddWorktree() {
	o := a.opened()
	if o == nil {
		return
	}
	view, err := newWorktreeView(a.eng)
	if err != nil {
		a.log.Warn("open worktree dialog failed", "error", err)
		return
	}
	view.SetKnown(a.knownWorktreeBranches(o))
	view.SetParentDirectory(filepath.Dir(o.path))
	a.wireWorktreeView(view)
	a.showModal(view.Dialog(), view)
}

func (a *App) knownWorktreeBranches(o *openedRepository) worktree.Known {
	known := worktree.Known{}
	if snap, err := loadBranchSnapshot(o.store); err == nil {
		known.Head = snap.Current
		for _, branch := range snap.Local {
			known.Branches = append(known.Branches, branch.Name.Short())
		}
	} else {
		a.log.Warn("read branches for worktree dialog failed", "error", err)
	}
	list, err := listWorktrees(o.repo)
	if err != nil {
		a.log.Warn("list worktrees failed", "error", err)
		return known
	}
	for _, wt := range list {
		if wt.Branch != "" {
			known.CheckedOut = append(known.CheckedOut, wt.Branch.Short())
		}
	}
	return known
}

func (a *App) wireWorktreeView(view *worktree.View) {
	view.OnOK = func(req worktree.Request) {
		a.eng.CloseModal(view.Dialog())
		a.startAddWorktree(req)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
}

func (a *App) startAddWorktree(req worktree.Request) {
	o := a.opened()
	if o == nil {
		return
	}
	parentID := a.worktreeParentID()
	a.RunOperation(i18n.T("Operation.Title.AddWorktree"), func(ctx context.Context, reporter OperationReporter) error {
		opts := worktreeAddOptions(req, newOperationProgress(reporter))
		wt, err := addWorktree(ctx, o.repo, req.Path, opts)
		if err != nil {
			return err
		}
		a.Post(func() { a.registerAddedWorktree(parentID, wt.Path) })
		return nil
	})
}

func worktreeAddOptions(req worktree.Request, prog progress.Func) ops.AddWorktreeOptions {
	opts := ops.AddWorktreeOptions{
		StartPoint: strings.TrimSpace(req.StartPoint),
		NoCheckout: req.NoCheckout,
		Progress:   prog,
	}
	switch req.Mode {
	case worktree.ModeExistingBranch:
		opts.StartPoint = strings.TrimSpace(req.Branch)
	case worktree.ModeDetached:
		opts.Detach = true
	default:
		opts.Branch = strings.TrimSpace(req.Branch)
	}
	return opts
}

func (a *App) registerAddedWorktree(parentID, path string) {
	o := a.opened()
	if o == nil {
		return
	}
	a.saveWorktreeTree(parentID, o)
	node, ok := a.registry.FindByPath(path)
	if !ok {
		return
	}
	a.ActivateRepository(node.ID)
}

func (a *App) saveWorktreeTree(parentID string, o *openedRepository) {
	if !a.syncWorktrees(parentID, o.repo) {
		return
	}
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
	a.reposView.Render(a.registry, a.repoTreeState())
}

func (a *App) worktreeParentID() string {
	node, ok := a.registry.Active()
	if !ok {
		return ""
	}
	if node.Kind != repo.KindWorktree {
		return node.ID
	}
	parent, ok := a.registry.ParentOf(node.ID)
	if !ok {
		return ""
	}
	return parent.ID
}

func (a *App) removeActiveWorktree() {
	node, ok := a.registry.Active()
	if !ok || node.Kind != repo.KindWorktree {
		return
	}
	parentID := a.worktreeParentID()
	if parentID == "" {
		return
	}
	path := node.Path
	a.askConfirm(i18n.T("Dialog.Worktree.Remove.Title"), i18n.Tf("Dialog.Worktree.Remove.Message", path), func(confirmed bool) {
		if !confirmed {
			return
		}
		a.dropWorktree(parentID, path, false)
	})
}

func (a *App) dropWorktree(parentID, path string, force bool) {
	a.ActivateRepository(parentID)
	o := a.opened()
	if o == nil {
		return
	}
	err := removeWorktree(context.Background(), o.repo, path, force)
	if errors.Is(err, ops.ErrWorktreeDirty) && !force {
		a.askConfirm(i18n.T("Dialog.Worktree.Remove.Title"), i18n.Tf("Dialog.Worktree.Remove.Dirty", path), func(confirmed bool) {
			if !confirmed {
				return
			}
			a.dropWorktree(parentID, path, true)
		})
		return
	}
	if err != nil {
		a.log.Warn("remove worktree failed", "path", path, "error", err)
		a.showError(i18n.T("Dialog.Worktree.Remove.Title"), i18n.Tf("Dialog.Worktree.Remove.Failed", err))
		return
	}
	a.saveWorktreeTree(parentID, o)
}

func (a *App) pruneObsoleteWorktrees() {
	o := a.opened()
	if o == nil {
		return
	}
	title := i18n.T("Dialog.Worktree.Prune.Title")
	stale, err := pruneWorktrees(o.repo, ops.PruneWorktreesOptions{DryRun: true})
	if err != nil {
		a.log.Warn("prune worktrees failed", "error", err)
		a.showError(title, i18n.Tf("Dialog.Worktree.Prune.Failed", err))
		return
	}
	if len(stale) == 0 {
		a.showInfo(title, i18n.T("Dialog.Worktree.Prune.Nothing"))
		return
	}
	a.askConfirm(title, i18n.Tf("Dialog.Worktree.Prune.Message", strings.Join(stale, "\n")), func(confirmed bool) {
		if !confirmed {
			return
		}
		a.applyWorktreePrune(o, title)
	})
}

func (a *App) applyWorktreePrune(o *openedRepository, title string) {
	pruned, err := pruneWorktrees(o.repo, ops.PruneWorktreesOptions{})
	if err != nil {
		a.log.Warn("prune worktrees failed", "error", err)
		a.showError(title, i18n.Tf("Dialog.Worktree.Prune.Failed", err))
		return
	}
	a.saveWorktreeTree(a.worktreeParentID(), o)
	a.showInfo(title, i18n.Tf("Dialog.Worktree.Prune.Done", len(pruned)))
}
