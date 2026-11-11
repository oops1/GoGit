package app

import (
	"github.com/oops1/headless-gui/v4/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/conflict"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

func (a *App) rememberOnClose(dlg *widget.Dialog, remember func()) {
	closed := dlg.OnClosed
	dlg.OnClosed = func() {
		if closed != nil {
			closed()
		}
		a.Post(func() {
			remember()
			if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
				a.log.Warn("save window size failed", "error", err)
			}
		})
	}
}

func rememberSize(dlg *widget.Dialog, target *config.WindowSize) {
	if width, height, ok := dialogs.RememberedSize(dlg); ok {
		*target = config.WindowSize{Width: width, Height: height}
	}
}

func (a *App) restoreCompareWindow(dlg *widget.Dialog) {
	dialogs.RestoreSize(dlg, a.cfg.UI.CompareWindow.Width, a.cfg.UI.CompareWindow.Height)
	a.rememberOnClose(dlg, func() { rememberSize(dlg, &a.cfg.UI.CompareWindow) })
}

func (a *App) restoreConflictWindow(view *conflict.View) {
	saved := a.cfg.UI.ConflictWindow
	dialogs.RestoreSize(view.Dialog(), saved.Width, saved.Height)
	view.SetPanes(!saved.HideBase, !saved.SeparateScroll)
	a.rememberOnClose(view.Dialog(), func() {
		rememberSize(view.Dialog(), &a.cfg.UI.ConflictWindow.WindowSize)
		a.cfg.UI.ConflictWindow.HideBase = !view.ShowsBase()
		a.cfg.UI.ConflictWindow.SeparateScroll = !view.ScrollsTogether()
	})
}
