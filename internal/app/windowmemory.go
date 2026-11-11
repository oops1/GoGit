package app

import (
	"github.com/oops1/headless-gui/v4/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/compare"
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

func (a *App) restoreCompareWindow(view *compare.View) {
	saved := a.cfg.UI.CompareWindow
	dialogs.RestoreSize(view.Dialog(), saved.Width, saved.Height)
	view.SetLayout(compare.Layout{CollapseLeft: saved.CollapseLeft, CollapseRight: saved.CollapseRight, Split: saved.Split})
	a.rememberOnClose(view.Dialog(), func() {
		target := &a.cfg.UI.CompareWindow
		rememberSize(view.Dialog(), &target.WindowSize)
		layout := view.Layout()
		target.CollapseLeft, target.CollapseRight, target.Split = layout.CollapseLeft, layout.CollapseRight, layout.Split
	})
}

func (a *App) restoreConflictWindow(view *conflict.View) {
	saved := a.cfg.UI.ConflictWindow
	dialogs.RestoreSize(view.Dialog(), saved.Width, saved.Height)
	view.SetPanes(!saved.HideBase, !saved.SeparateScroll)
	view.SetLayout(conflict.Layout{
		CollapseOurs:     saved.CollapseOurs,
		CollapseBase:     saved.CollapseBase,
		CollapseTheirs:   saved.CollapseTheirs,
		ResultMaximized:  saved.ResultMaximized,
		SourcesMaximized: saved.SourcesMaximized,
		Split:            saved.Split,
	})
	a.rememberOnClose(view.Dialog(), func() {
		target := &a.cfg.UI.ConflictWindow
		rememberSize(view.Dialog(), &target.WindowSize)
		target.HideBase = !view.ShowsBase()
		target.SeparateScroll = !view.ScrollsTogether()
		layout := view.Layout()
		target.CollapseOurs, target.CollapseBase, target.CollapseTheirs = layout.CollapseOurs, layout.CollapseBase, layout.CollapseTheirs
		target.ResultMaximized, target.SourcesMaximized, target.Split = layout.ResultMaximized, layout.SourcesMaximized, layout.Split
	})
}
