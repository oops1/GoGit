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
	pane := a.Dock().FindPane(paneDetails)
	tabs, ok := a.named["detailsTabs"].(*widget.TabControl)
	if pane == nil || !ok {
		return
	}
	pane.OnStateChanged = func(*widget.DockPane) { a.keepDetailsTabsVisible() }
	width := 0
	if side := pane.Side(); side == widget.DockLeft || side == widget.DockRight {
		width = detailsTabsWidth(tabs)
	}
	if pane.MinSize != width {
		pane.SetMinSize(width)
	}
}
