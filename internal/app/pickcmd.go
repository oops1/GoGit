package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
)

var runCherryPick = ops.CherryPick

var runRevert = ops.Revert

func (a *App) pickItems(id hash.ObjectID) []widget.MenuItem {
	enabled := a.State().Enabled(CmdMerge)
	pick := menuItem("Menu.Context.CherryPick", func() { a.pickCommit(id, false) })
	revert := menuItem("Menu.Context.Revert", func() { a.pickCommit(id, true) })
	pick.Disabled, revert.Disabled = !enabled, !enabled
	return []widget.MenuItem{menuSeparator(), pick, revert}
}

func (a *App) pickCommit(id hash.ObjectID, revert bool) {
	run, done := runCherryPick, "Status.Picked"
	if revert {
		run, done = runRevert, "Status.Reverted"
	}
	var result ops.PickResult
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		var err error
		result, err = run(ctx, r, id.String(), ops.PickOptions{})
		return err
	}, func(err error) {
		switch {
		case err != nil:
			a.log.Warn("apply commit failed", "commit", id.String(), "revert", revert, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.PickFailed", err))
		case !result.Clean():
			a.statusLabel.SetText(i18n.Tf("Status.PickConflicts", len(result.Conflicts)))
		default:
			a.statusLabel.SetText(i18n.Tf(done, shortHash(result.Commit)))
		}
		a.RefreshRepository()
	})
}
