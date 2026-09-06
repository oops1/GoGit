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
		btn := a.named[toolbarButtons[id]].(*widget.Button)
		btn.Icon = icons.ToolbarPlain(name, toolbarIconSize)
		btn.IconSize = toolbarIconSize
		if captions {
			btn.IconPos = widget.IconTop
		} else {
			btn.IconPos = widget.IconOnly
		}
		resizeToolbarButton(btn, width, height)
	}
	a.relayoutToolbar()
}

func (a *App) toolbarCaptionsWidth() int {
	width := toolbarButtonMinWidth
	for _, name := range toolbarButtons {
		btn := a.named[name].(*widget.Button)
		if w := toolbarCaptionButtonWidth(btn.Text); w > width {
			width = w
		}
	}
	if width > toolbarButtonMaxWidth {
		width = toolbarButtonMaxWidth
	}
	return width
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
