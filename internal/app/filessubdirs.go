package app

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
)

const filesSubdirsButton = "filesFilterSubdirs"

func (a *App) filesSubdirsButton() *widget.Button {
	return a.named[filesSubdirsButton].(*widget.Button)
}

func (a *App) wireFilesSubdirsButton() {
	a.filesSubdirsButton().OnClick = a.toggleFilesSubdirectories
}

func (a *App) toggleFilesSubdirectories() {
	a.cfg.UI.FilesSubdirectories = !a.cfg.UI.FilesSubdirectories
	a.applyFilesSubdirsButtonVisuals(themeFor(a.EffectiveTheme()))
	a.applyFilesFilter()
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}

func (a *App) applyFilesSubdirsButtonVisuals(t *widget.Theme) {
	btn := a.filesSubdirsButton()
	btn.IconSize = filesStatusIconSize
	btn.IconPos = widget.IconOnly
	btn.HoverBG = t.BtnHoverBG
	btn.PressedBG = t.BtnPressedBG
	btn.SetToolTip(i18n.T("Files.Filter.Subdirectories"))
	if a.cfg.UI.FilesSubdirectories {
		btn.Icon = icons.ToolbarPlain(filesSubdirsIcon, filesStatusIconSize)
		btn.Background = translucent(t.Accent, filesStatusEnabledBGAlpha)
		btn.BorderColor = t.Accent
		return
	}
	btn.Icon = icons.ToolbarMuted(filesSubdirsIcon, filesStatusIconSize)
	btn.Background = color.RGBA{}
	btn.BorderColor = t.BtnBorder
}

const filesSubdirsIcon = "subdirs"
