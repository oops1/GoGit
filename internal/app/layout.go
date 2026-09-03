package app

import (
	"errors"
	"io/fs"
	"os"

	"github.com/oops1/headless-gui/v3/widget"
)

func (a *App) RestoreLayout() error {
	data, err := a.layoutStore.Load(a.paths.LayoutFile())
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}
	return a.Dock().RestoreLayout(data)
}

func (a *App) SaveLayout() error {
	w, h := a.eng.CanvasSize()
	a.cfg.Window.Width = w
	a.cfg.Window.Height = h
	if err := a.layoutStore.Save(a.paths.LayoutFile(), a.Dock().SaveLayout()); err != nil {
		return err
	}
	return a.cfg.Save(a.paths.ConfigFile())
}

func (a *App) ResetLayout() error {
	_ = a.Dock().RestoreLayout(a.defaultLayout)
	err := os.Remove(a.paths.LayoutFile())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (a *App) PaneVisible(id string) bool {
	p := a.Dock().FindPane(id)
	if p == nil {
		return false
	}
	return p.State() != widget.PaneClosed
}

func (a *App) SetPaneVisible(id string, visible bool) {
	p := a.Dock().FindPane(id)
	if p == nil {
		return
	}
	if visible {
		p.Show()
	} else {
		p.Close()
	}
}
