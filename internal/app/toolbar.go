package app

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	toolbarIconSize       = 24
	toolbarButtonWidth    = 76
	toolbarButtonHeight   = 54
	toolbarCompactWidth   = 44
	toolbarCompactHeight  = 36
	toolbarCompactIconPad = 0
)

func (a *App) applyToolbarIcons(*widget.Theme) {
	captions := a.cfg.UI.ToolbarCaptions
	for id, name := range toolbarIcons {
		btn := a.named[toolbarButtons[id]].(*widget.Button)
		btn.Icon = icons.ToolbarPlain(name, toolbarIconSize)
		btn.IconSize = toolbarIconSize
		if captions {
			btn.IconPos = widget.IconTop
			resizeToolbarButton(btn, toolbarButtonWidth, toolbarButtonHeight)
			continue
		}
		btn.IconPos = widget.IconOnly
		resizeToolbarButton(btn, toolbarCompactWidth, toolbarCompactHeight)
	}
}

func resizeToolbarButton(btn *widget.Button, w, h int) {
	b := btn.Bounds()
	btn.SetBounds(rectOfSize(b.Min.X, b.Min.Y, w, h))
}

func rectOfSize(x, y, w, h int) image.Rectangle {
	return image.Rect(x, y, x+w, y+h)
}
