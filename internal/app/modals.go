package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/dialogs"
)

type styledView interface {
	Restyle(*widget.Theme)
}

type dialogOwner interface {
	OwnedDialogs() []*widget.Dialog
}

type shownDialog struct {
	modal widget.ModalWidget
	trees []widget.Widget
}

func (a *App) showModal(modal widget.ModalWidget, view styledView) {
	a.releaseClosedDialogs()
	a.trackDialog(modal, view)
	a.eng.ShowModal(modal)
	view.Restyle(a.theme())
}

func (a *App) trackDialog(modal widget.ModalWidget, view styledView) {
	shown := shownDialog{modal: modal, trees: []widget.Widget{modal}}
	if owner, ok := view.(dialogOwner); ok {
		for _, dlg := range owner.OwnedDialogs() {
			shown.trees = append(shown.trees, dlg)
		}
	}
	a.dialogsMu.Lock()
	a.shownDialogs = append(a.shownDialogs, shown)
	a.dialogsMu.Unlock()
}

func (a *App) releaseClosedDialogs() {
	a.releaseDialogs(false)
}

func (a *App) releaseDialogs(all bool) {
	a.dialogsMu.Lock()
	var released, kept []shownDialog
	for _, shown := range a.shownDialogs {
		if all || !shown.modal.IsModal() {
			released = append(released, shown)
			continue
		}
		kept = append(kept, shown)
	}
	a.shownDialogs = kept
	a.dialogsMu.Unlock()
	for _, shown := range released {
		for _, tree := range shown.trees {
			dialogs.Release(tree)
		}
	}
}
