package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/reset"
)

var newResetView = reset.NewView

var runReset = ops.Reset

var resetModes = map[reset.Mode]ops.ResetMode{
	reset.ModeSoft:  ops.ResetSoft,
	reset.ModeMixed: ops.ResetMixed,
	reset.ModeHard:  ops.ResetHard,
}

func (a *App) resetItems(id hash.ObjectID) []widget.MenuItem {
	item := menuItem("Menu.Context.Reset", func() { a.openReset(id) })
	item.Disabled = !a.State().Enabled(CmdMerge)
	return []widget.MenuItem{item}
}

func (a *App) openReset(id hash.ObjectID) {
	o := a.opened()
	if o == nil {
		return
	}
	view, err := newResetView()
	if err != nil {
		a.log.Warn("open reset dialog failed", "error", err)
		return
	}
	view.SetKnown(reset.Known{Branch: a.currentBranchName(), Commit: shortHash(id)}, reset.ModeMixed)
	view.OnOK = func(mode reset.Mode) {
		a.eng.CloseModal(view.Dialog())
		a.startReset(id, mode)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) startReset(id hash.ObjectID, mode reset.Mode) {
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		_, err := runReset(ctx, r, id.String(), ops.ResetOptions{Mode: resetModes[mode]})
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("reset failed", "commit", id.String(), "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.ResetFailed", err))
		} else {
			a.statusLabel.SetText(i18n.Tf("Status.Reset", shortHash(id)))
		}
		a.RefreshRepository()
	})
}
