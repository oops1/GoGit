package app

import (
	"github.com/oops1/gogit/internal/ui/settings"
)

type visibilityToggler interface {
	SetVisible(bool)
	IsVisible() bool
}

var newSettingsView = settings.NewView

func (a *App) openSettings() {
	a.showSettings(settings.FromConfig(a.cfg), a.applySettings)
}

func (a *App) applySettings(m settings.Model, ok bool) {
	if !ok {
		return
	}
	m.ApplyTo(a.cfg)
	a.SetLanguage(a.cfg.Language)
	a.SetTheme(a.cfg.Theme)
	a.setToolbarVisible(a.cfg.UI.ShowToolbar)
	a.setStatusBarVisible(a.cfg.UI.ShowStatusBar)
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}

func (a *App) setToolbarVisible(visible bool) {
	if w, ok := a.named["toolbar"].(visibilityToggler); ok {
		w.SetVisible(visible)
	}
}

func (a *App) setStatusBarVisible(visible bool) {
	if w, ok := a.named["statusBar"].(visibilityToggler); ok {
		w.SetVisible(visible)
	}
}

func (a *App) defaultShowSettings(initial settings.Model, cb func(settings.Model, bool)) {
	view, err := newSettingsView(a.eng, a.languages, initial)
	if err != nil {
		a.log.Warn("open settings dialog failed", "error", err)
		return
	}
	a.wireSettingsView(view, cb)
	a.eng.ShowModal(view.Dialog())
}

func (a *App) wireSettingsView(view *settings.View, cb func(settings.Model, bool)) {
	view.OnOK = func(m settings.Model) {
		a.eng.CloseModal(view.Dialog())
		cb(m, true)
	}
	view.OnCancel = func() {
		a.eng.CloseModal(view.Dialog())
		cb(settings.Model{}, false)
	}
}
