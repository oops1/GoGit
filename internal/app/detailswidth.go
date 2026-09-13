package app

import (
	"github.com/oops1/headless-gui/v3/widget"
)

const detailsTabsMargin = 12

func detailsTabsWidth(tabs *widget.TabControl) int {
	width := detailsTabsMargin
	for i := range tabs.TabCount() {
		if tabs.IsTabVisible(i) {
			width += widget.MeasureUIText(tabs.TabHeader(i), widget.DefaultFontSizePt) + tabs.TabPadH*2
		}
	}
	return width
}

func (a *App) keepDetailsTabsVisible() {
	dock := a.Dock()
	pane := dock.FindPane(paneDetails)
	tabs, ok := a.named["detailsTabs"].(*widget.TabControl)
	if pane == nil || !ok {
		return
	}
	side := pane.Side()
	if side != widget.DockLeft && side != widget.DockRight {
		return
	}
	if need := detailsTabsWidth(tabs); dock.SideSize(side) < need {
		dock.SetSideSize(side, need)
		dock.SetBounds(dock.Bounds())
	}
}
