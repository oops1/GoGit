package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/icons"
)

const toolbarIconSize = 16

func (a *App) applyToolbarIcons(t *widget.Theme) {
	for id, name := range toolbarIcons {
		btn := a.named[toolbarButtons[id]].(*widget.Button)
		btn.Icon = icons.Toolbar(name, toolbarIconSize, t.BtnText)
		btn.IconSize = toolbarIconSize
		btn.IconPos = widget.IconLeft
	}
}
