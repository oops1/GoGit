package app

import (
	"context"
	"errors"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/stash"
	"github.com/oops1/gogit/internal/ui/switchchanges"
)

var newStashView = stash.NewView

var runStashApply = ops.StashApply

var runStashPop = ops.StashPop

var runStashDrop = ops.StashDrop

func (a *App) registerStashHandlers() {
	a.handlers[CmdStashSave] = a.openSaveStash
	a.handlers[CmdStashApply] = func() { a.openStashDialog(stash.ModeApply, 0) }
	a.handlers[CmdStashDrop] = func() { a.openStashDialog(stash.ModeDrop, 0) }
}

func (a *App) refMenu(ref refs.Name) []widget.MenuItem {
	if index, ok := branches.StashIndex(ref); ok {
		return a.stashMenu(index)
	}
	return a.branchMenu(ref)
}

func (a *App) activateRef(ref refs.Name) {
	if index, ok := branches.StashIndex(ref); ok {
		a.openStashDialog(stash.ModeApply, index)
		return
	}
	a.checkOutRef(ref)
}

func (a *App) stashMenu(index int) []widget.MenuItem {
	return []widget.MenuItem{
		menuItem("Menu.Stash.Apply", func() { a.applyStash(index, false) }),
		menuItem("Menu.Stash.ApplyDrop", func() { a.applyStash(index, true) }),
		menuSeparator(),
		menuItem("Menu.Stash.Drop", func() { a.confirmDropStash(index) }),
	}
}

func stashSelector(index int) string {
	return branches.StashRef(index).String()
}

func stashLabels(entries []branches.Stash) []string {
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		labels = append(labels, stashSelector(entry.Index)+": "+entry.Message)
	}
	return labels
}

func (a *App) openSaveStash() {
	if a.opened() == nil {
		return
	}
	a.askInput(i18n.T("Dialog.SaveStash.Title"), i18n.T("Dialog.SaveStash.Prompt"), func(text string, ok bool) {
		if ok {
			a.saveStash(strings.TrimSpace(text))
		}
	})
}

func (a *App) saveStash(message string) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T("Operation.Title.SaveStash"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		_, err = runStashPush(ctx, r, ops.StashOptions{Message: message})
		switch {
		case errors.Is(err, ops.ErrNothingToStash):
			reporter.Log(i18n.T("Operation.Log.NothingToStash"))
			return nil
		case err != nil:
			return err
		}
		reporter.Log(i18n.Tf("Operation.Log.StashSaved", stashSelector(0)))
		return nil
	})
}

func (a *App) openStashDialog(mode stash.Mode, selected int) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read stashes failed", "error", err)
		return
	}
	view, err := newStashView(mode)
	if err != nil {
		a.log.Warn("open stash dialog failed", "error", err)
		return
	}
	view.SetEntries(stashLabels(snap.Stashes), selected)
	view.OnOK = func(req stash.Request) {
		a.eng.CloseModal(view.Dialog())
		if mode == stash.ModeDrop {
			a.dropStash(req.Index)
			return
		}
		a.applyStash(req.Index, req.Drop)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) applyStash(index int, drop bool) {
	o := a.opened()
	if o == nil {
		return
	}
	apply := runStashApply
	if drop {
		apply = runStashPop
	}
	a.RunOperation(i18n.T("Operation.Title.ApplyStash"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := apply(ctx, r, index, ops.StashApplyOptions{})
		reportStashApply(reporter, stashSelector(index), result, err)
		return err
	})
}

func reportStashApply(reporter OperationReporter, selector string, result ops.StashApplyResult, err error) {
	var overwrite *ops.OverwriteError
	switch {
	case errors.As(err, &overwrite):
		reporter.Log(i18n.Tf("Operation.Log.StashBlocked", overwriteList(overwrite)))
	case err != nil:
	case !result.Clean():
		reporter.Log(i18n.Tf("Operation.Log.StashConflicts", switchchanges.ListPaths(result.Conflicts)))
	default:
		reporter.Log(i18n.Tf("Operation.Log.StashApplied", selector))
		if result.Dropped {
			reporter.Log(i18n.Tf("Operation.Log.StashDropped", selector))
		}
	}
}

func (a *App) confirmDropStash(index int) {
	a.askConfirm(i18n.T("Dialog.DropStash.Title"), i18n.Tf("Dialog.DropStash.Message", stashSelector(index)), func(ok bool) {
		if ok {
			a.dropStash(index)
		}
	})
}

func (a *App) dropStash(index int) {
	selector := stashSelector(index)
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return runStashDrop(ctx, r, index)
	}, func(err error) {
		if err != nil {
			a.log.Warn("drop stash failed", "stash", selector, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.StashDropFailed", err))
		} else {
			a.statusLabel.SetText(i18n.Tf("Status.StashDropped", selector))
		}
		a.RefreshRepository()
	})
}
