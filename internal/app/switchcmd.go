package app

import (
	"context"
	"errors"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/switchbranch"
)

var newSwitchView = switchbranch.NewView

var runStartBranch = ops.StartBranch

var runSwitchBranch = ops.Switch

func (a *App) registerSwitchHandlers() {
	a.handlers[CmdSwitch] = func() { a.openSwitch("") }
}

func (a *App) switchItems(ref refs.Name) []widget.MenuItem {
	if !ref.IsBranch() && !ref.IsRemote() {
		return nil
	}
	if ref == refs.BranchName(a.currentBranchName()) {
		return nil
	}
	item := menuItem("Menu.Context.SwitchHere", func() { a.openSwitch(ref.Short()) })
	item.Disabled = !a.State().Enabled(CmdSwitch)
	return []widget.MenuItem{item}
}

func (a *App) openSwitch(selected string) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for the switch dialog failed", "error", err)
		return
	}
	view, err := newSwitchView()
	if err != nil {
		a.log.Warn("open switch dialog failed", "error", err)
		return
	}
	view.SetKnown(switchKnown(snap), selected)
	view.OnOK = func(choice switchbranch.Choice) {
		a.eng.CloseModal(view.Dialog())
		a.startSwitch(choice)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func switchKnown(snap branches.Snapshot) switchbranch.Known {
	known := switchbranch.Known{Current: snap.Current}
	for _, branch := range snap.Local {
		known.Local = append(known.Local, branch.Name.Short())
	}
	for _, remote := range snap.Remotes {
		for _, branch := range remote.Branches {
			known.Remote = append(known.Remote, branch.Name.Short())
		}
	}
	return known
}

func (a *App) startSwitch(choice switchbranch.Choice) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T("Operation.Title.Switch"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		name, err := switchTo(ctx, r, choice)
		reportSwitch(reporter, name, err)
		return err
	})
}

func switchTo(ctx context.Context, r *gitrepo.Repository, choice switchbranch.Choice) (string, error) {
	if !choice.StartsABranch() {
		return choice.Source, runSwitchBranch(ctx, r, choice.Source, ops.SwitchOptions{})
	}
	result, err := runStartBranch(ctx, r, choice.Name, choice.Source, ops.StartBranchOptions{Track: choice.Track})
	return result.Branch.Short(), err
}

func reportSwitch(reporter OperationReporter, name string, err error) {
	var overwrite *ops.OverwriteError
	switch {
	case errors.As(err, &overwrite):
		reporter.Log(i18n.Tf("Operation.Log.SwitchBlocked", overwriteList(overwrite)))
	case err != nil:
	default:
		reporter.Log(i18n.Tf("Operation.Log.Switched", name))
	}
}
