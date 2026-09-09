package app

import (
	"github.com/oops1/headless-gui/v3/widget"
)

type styledView interface {
	Restyle(*widget.Theme)
}

func (a *App) showModal(modal widget.ModalWidget, view styledView) {
	a.eng.ShowModal(modal)
	view.Restyle(a.theme())
}
