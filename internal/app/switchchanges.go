package app

import (
	"context"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/switchchanges"
)

var newSwitchChangesView = switchchanges.NewView

var runStashPush = ops.StashPush

var runSwitchMerging = ops.SwitchMerging

func (a *App) askSwitchChanges(target string, paths []string) {
	view, err := newSwitchChangesView()
	if err != nil {
		a.log.Warn("open switch changes dialog failed", "error", err)
		return
	}
	view.SetBlocked(target, paths)
	view.OnChoose = func(choice switchchanges.Choice) {
		a.eng.CloseModal(view.Dialog())
		if choice.Remember {
			a.rememberSwitchChanges(choice.Mode)
		}
		a.switchCarryingChanges(target, choice.Mode, paths)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) rememberSwitchChanges(mode string) {
	a.cfg.Git.SwitchChanges = mode
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}

func (a *App) switchCarryingChanges(target, mode string, paths []string) {
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
		return resolveBlockedSwitch(ctx, r, reporter, target, mode, paths)
	})
}

func resolveBlockedSwitch(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter, target, mode string, paths []string) error {
	switch mode {
	case config.SwitchChangesStash:
		if _, err := runStashPush(ctx, r, ops.StashOptions{}); err != nil {
			return err
		}
		reporter.Log(i18n.Tf("Operation.Log.SwitchStashed", ops.StashEntry{}.Selector()))
		err := runSwitchBranch(ctx, r, target, ops.SwitchOptions{})
		reportSwitch(reporter, target, err)
		return err
	case config.SwitchChangesOverwrite:
		err := runSwitchBranch(ctx, r, target, ops.SwitchOptions{Force: true})
		if err == nil {
			reporter.Log(i18n.Tf("Operation.Log.SwitchOverwritten", switchchanges.ListPaths(paths)))
		}
		reportSwitch(reporter, target, err)
		return err
	default:
		result, err := runSwitchMerging(ctx, r, target)
		reportSwitch(reporter, target, err)
		switch {
		case err != nil:
		case !result.Clean():
			reporter.Log(i18n.Tf("Operation.Log.SwitchCarryConflicts", switchchanges.ListPaths(result.Conflicts)))
		case result.Stashed:
			reporter.Log(i18n.Tf("Operation.Log.SwitchCarried", target))
		}
		return err
	}
}
