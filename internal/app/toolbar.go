package app

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	toolbarIconSize       = 24
	toolbarButtonMinWidth = 76
	toolbarButtonMaxWidth = 170
	toolbarButtonIconGap  = 4
	toolbarButtonPadding  = 14
	toolbarButtonHeight   = 54
	toolbarCompactWidth   = 44
	toolbarCompactHeight  = 36
	toolbarCompactIconPad = 0
)

func (a *App) applyToolbarIcons(*widget.Theme) {
	captions := a.cfg.UI.ToolbarCaptions
	width, height := toolbarCompactWidth, toolbarCompactHeight
	if captions {
		width, height = a.toolbarCaptionsWidth(), toolbarButtonHeight
	}
	for id, name := range toolbarIcons {
		styleToolbarButton(a.named[toolbarButtons[id]].(*widget.Button), name, captions, width, height)
	}
	a.styleToolbarMenus(captions, width, height)
	a.relayoutToolbar()
}

func styleToolbarButton(btn *widget.Button, icon string, captions bool, width, height int) {
	btn.Icon = icons.ToolbarPlain(icon, toolbarIconSize)
	btn.IconSize = toolbarIconSize
	if captions {
		btn.IconPos = widget.IconTop
	} else {
		btn.IconPos = widget.IconOnly
	}
	resizeToolbarButton(btn, width, height)
}

func (a *App) toolbarCaptionsWidth() int {
	width := toolbarButtonMinWidth
	for _, w := range a.toolbarCaptionWidths() {
		width = max(width, w)
	}
	return min(width, toolbarButtonMaxWidth)
}

func toolbarCaptionButtonWidth(text string) int {
	w := toolbarIconSize + toolbarButtonIconGap + widget.MeasureUIText(text, widget.DefaultFontSizePt) + toolbarButtonPadding
	if w < toolbarButtonMinWidth {
		return toolbarButtonMinWidth
	}
	return w
}

func (a *App) relayoutToolbar() {
	panel, ok := a.named["toolbar"].(*widget.StackPanel)
	if !ok {
		return
	}
	panel.SetBounds(panel.Bounds())
}

func resizeToolbarButton(btn *widget.Button, w, h int) {
	b := btn.Bounds()
	btn.SetBounds(rectOfSize(b.Min.X, b.Min.Y, w, h))
}

func rectOfSize(x, y, w, h int) image.Rectangle {
	return image.Rect(x, y, x+w, y+h)
}
